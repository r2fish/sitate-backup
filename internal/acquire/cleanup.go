package acquire

// RunCleanup: 取得アダプタの容器の後片付け(spec 3.1.1 の規則 13、7.5)。容器には実行の run_id のラベル sitate-run=<run_id> が付く(AdapterRunner が付ける)。

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// RunLabel は容器に付けるラベルの名前(値は run_id)。
const RunLabel = "sitate-run"

// errListFailed は容器の一覧を取れなかった(実行環境はあるが動かない)ことの印。
var errListFailed = errors.New("容器の一覧を取れない")

// RunCleanup は contract.RunCleanup の実物。Podman は容器の実行環境の実行ファイル(空なら "podman")。
type RunCleanup struct{ Podman string }

var _ contract.RunCleanup = (*RunCleanup)(nil)

func (r *RunCleanup) bin() string {
	if r.Podman != "" {
		return r.Podman
	}
	return "podman"
}

// psArgs は podman ps の引数列。--filter は 1 回の ps に重ねる(同じ名前のフィルタは OR、名前をまたぐと AND)。
func (r *RunCleanup) psArgs(filters []string) []string {
	a := []string{"ps", "-a", "--no-trunc"}
	for _, f := range filters {
		a = append(a, "--filter", f)
	}
	return append(a, "--format", "{{.ID}}")
}

// listContainers はフィルタに合う容器の ID(停止済みを含む)。実行環境が無ければ ok が偽(片付ける容器が無い)。
func (r *RunCleanup) listContainers(ctx context.Context, filters ...string) (ids []string, ok bool, err error) {
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, r.bin(), r.psArgs(filters)...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		var ee *exec.Error
		if errors.As(err, &ee) {
			return nil, false, nil
		}
		return nil, true, errListFailed
	}
	return strings.Fields(out.String()), true, nil
}

// Cleanup は contract.RunCleanup を満たす。own・interrupted の run_id のラベルの容器を消す。interrupted が空(印が読めず run_id が分からない)なら、
// sitate-run ラベルの付いた停止済みの容器をすべて消す(spec 7.5)。消した容器は cleaned、消せなかったものは unverified(どちらも空でも配列)。
func (r *RunCleanup) Cleanup(ctx context.Context, own string, interrupted string) (cleaned, unverified []contract.Resource, err error) {
	cleaned, unverified = []contract.Resource{}, []contract.Resource{}
	var queries [][]string // それぞれが 1 回の ps(中のフィルタは AND)
	for _, id := range []string{own, interrupted} {
		if id != "" {
			queries = append(queries, []string{"label=" + RunLabel + "=" + id})
		}
	}
	if interrupted == "" {
		// run_id が分からないときは、sitate-run ラベルの付いた停止済みの容器だけ(動いている容器や、ラベルの無い他人の容器は消さない)。
		queries = append(queries, []string{"label=" + RunLabel, "status=exited", "status=created"})
	}
	seen := map[string]bool{}
	for _, q := range queries {
		ids, ok, err := r.listContainers(ctx, q...)
		if errors.Is(err, errListFailed) { // 何が残っているか確かめられない: 破棄を確かめられなかった資源として知らせる
			return cleaned, append(unverified, contract.Resource{Kind: "container-runtime", ID: "unlistable"}), nil
		}
		if err != nil {
			return cleaned, unverified, err
		}
		if !ok {
			return cleaned, unverified, nil
		}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			if exec.CommandContext(ctx, r.bin(), "rm", "-f", id).Run() == nil {
				cleaned = append(cleaned, contract.Resource{Kind: "container", ID: id})
			} else {
				unverified = append(unverified, contract.Resource{Kind: "container", ID: id})
			}
		}
	}
	return cleaned, unverified, nil
}
