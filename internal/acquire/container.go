package acquire

// 取得アダプタの容器の起動(spec 3.4、3.2.2)。実行器(AdapterRunner)は署名と sha256 を検証した後、RunContext.Wrap が返す引数列を起動する。
// 容器は rootless の podman、ネットワーク名前空間なし、読み取り専用のルート、能力なし、非特権ユーザー、メモリの上限、run_id のラベル。
// ルートは核が用意する最小のもの(--rootfs。マウント点の空のファイルとディレクトリだけ)で、静的リンクの実行ファイルを読み取り専用で重ねる。鍵と秘密ファイルは渡さない。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 容器の中の置き場所。
const (
	containerAdapter = "/sitate/adapter"
	containerRecv    = "/sitate/recv"
	containerPipe    = "/run/sitate/pipe"
)

// Container は取得アダプタの容器の設定。Acquire.Container が nil なら容器を使わず、実行器が直接起動する(試験用)。
type Container struct {
	// Podman は実行ファイル(空なら "podman")。
	Podman string
	// RecvExe は SITATE_RECV の実体(静的リンクの実行ファイル。通常は核自身。EnvRecvPipe を見て RunRecvHelper を呼ぶ)。
	RecvExe string
	// Home・RuntimeDir は podman を動かすユーザーの HOME と XDG_RUNTIME_DIR(空なら核の環境の値)。
	Home, RuntimeDir string
	// ExtraEnv は容器へ足す環境変数 K=V(核の設定からだけ。試験の切り替え用。実行器の環境は許可リストを通る)。
	ExtraEnv []string
	// User は容器の中のユーザー(空なら "65534:65534")。
	User string
}

func (c *Container) podman() string {
	if c.Podman != "" {
		return c.Podman
	}
	return "podman"
}

func (c *Container) home() string {
	if c.Home != "" {
		return c.Home
	}
	return os.Getenv("HOME")
}

func (c *Container) runtimeDir() string {
	if c.RuntimeDir != "" {
		return c.RuntimeDir
	}
	return os.Getenv("XDG_RUNTIME_DIR")
}

// reap は時限切れや失敗の後に、この実行(run_id のラベル)の容器を止めて消す。実行器が止めるのは Wrap が返した子のグループだけで、
// 容器のクライアントを kill しても容器が残り得るため、同じ実行の中で acquire が責任を持つ。取り消された文脈とは別の締め切りで行う。
func (c *Container) reap(runID string) {
	ctx, cancel := context.WithTimeout(context.Background(), contract.RecordDeadline)
	defer cancel()
	_, _, _ = (&RunCleanup{Podman: c.Podman}).Cleanup(ctx, runID, runID)
}

func (c *Container) user() string {
	if c.User != "" {
		return c.User
	}
	return "65534:65534"
}

// taskDir は 1 つの対象の取得の作業ディレクトリの中身(ルート・パイプ)の置き場。
type taskDir struct {
	Root, Pipe string
	PrevIdx    string // 前回の索引の写し(無ければ空)。容器の中では containerWork/<名前>
}

// prepareRoot は最小のルートを作る(マウント点の空のファイルとディレクトリだけ)。
func prepareRoot(t taskDir, prevName string) error {
	for _, d := range []string{"sitate", "run/sitate/pipe", "run/sitate/work"} {
		if err := os.MkdirAll(filepath.Join(t.Root, filepath.FromSlash(d)), 0o755); err != nil {
			return err
		}
	}
	files := []string{"sitate/adapter", "sitate/recv", "run/sitate/pipe/" + pipeReqName, "run/sitate/pipe/" + pipeRepName}
	if t.PrevIdx != "" {
		files = append(files, "run/sitate/work/"+prevName)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(t.Root, filepath.FromSlash(f)), nil, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// args は podman run の引数列(exe は検証済みの取得アダプタの実行ファイル、env は子の環境 K=V)。
func (c *Container) args(runID string, t taskDir, prevName, exe string, env []string) ([]string, error) {
	if exe == "" || c.RecvExe == "" {
		return nil, errInternal("")
	}
	// 実行器の子の環境は PATH と LC_ALL だけなので、rootless の podman に要る HOME と XDG_RUNTIME_DIR は引数列の前の env で補う(パスだけ。秘密は入れない)。
	a := []string{}
	if h := c.home(); h != "" || c.runtimeDir() != "" {
		a = append(a, "env")
		if h != "" {
			a = append(a, "HOME="+h)
		}
		if x := c.runtimeDir(); x != "" {
			a = append(a, "XDG_RUNTIME_DIR="+x)
		}
	}
	a = append(a, c.podman(), "run", "--rm", "-i",
		"--network", "none", "--read-only", "--cap-drop", "all", "--security-opt", "no-new-privileges",
		"--user", c.user(), "--memory", strconv.FormatInt(contract.ContainerMemoryMax, 10),
		"--label", RunLabel+"="+runID, "--rootfs",
		"--mount", "type=bind,src="+exe+",dst="+containerAdapter+",ro",
		"--mount", "type=bind,src="+c.RecvExe+",dst="+containerRecv+",ro",
		"--mount", "type=bind,src="+t.Pipe+",dst="+containerPipe)
	if t.PrevIdx != "" {
		a = append(a, "--mount", "type=bind,src="+t.PrevIdx+",dst="+containerWork+"/"+prevName+",ro")
	}
	for _, e := range env {
		k, _, ok := strings.Cut(e, "=")
		if !ok || !envAllowed(k) {
			continue
		}
		a = append(a, "-e", e)
	}
	for _, e := range c.ExtraEnv {
		a = append(a, "-e", e)
	}
	a = append(a, "-e", contract.EnvRecv+"="+containerRecv, "-e", EnvRecvPipe+"="+containerPipe, t.Root, containerAdapter)
	return a, nil
}

// wrapFn は RunContext.Wrap に置く関数。
func (c *Container) wrapFn(runID string, t taskDir, prevName string) func(string, []string) ([]string, error) {
	return func(exe string, env []string) ([]string, error) {
		if err := prepareRoot(t, prevName); err != nil {
			return nil, fmt.Errorf("容器のルートを作れない: %w", err)
		}
		return c.args(runID, t, prevName, exe, env)
	}
}

// envAllowed は容器の中へ渡す環境変数の名前(PATH・LC_ALL と、実行器が許す試験の切り替えの変数。SITATE_RECV と EnvRecvPipe は Wrap が決める)。
func envAllowed(k string) bool {
	if k == "PATH" || k == "LC_ALL" {
		return true
	}
	return k != contract.EnvRecv && inList(contract.AdapterEnvAllowed, k)
}
