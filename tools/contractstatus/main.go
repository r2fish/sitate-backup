// Command contractstatus は契約試験の結果を WP ごとに「未実装 n / その他 m / skip k」で集計して出す(作業指示書 3.1、GB-102)。
//
// 割り当ては試験のパッケージのパスによる。「未実装」は失敗の原因が NOT_IMPLEMENTED の試験、「その他」はそれ以外の失敗、
// skip は「後のイテレーション」の印の付いた試験。app/test/contract/ の結果は -app <ファイル>(1 件 1 行の JSON)で受ける。
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const (
	markNotImpl = "NOT_IMPLEMENTED:"
	markLater   = "後のイテレーション:"
)

var assign = []struct{ prefix, wp string }{
	{"/internal/format", "U1a"}, {"/internal/store", "U1b"}, {"/internal/acquire", "U1c"}, {"/internal/approval", "U1d"},
	{"/internal/restore", "U1e"}, {"/internal/keys", "U1g"}, {"/internal/cli", "U1f-1"}, {"/internal/state", "U1f-1"},
	{"/internal/adapter", "U1f-0"}, {"/adapters/acquire/files", "U2a"}, {"/adapters/acquire/recv", "U2a"},
	{"/adapters/acquire/postgresql", "U2b"}, {"/adapters/restore-dest/ssh-host", "U2d"}, {"/adapters/notify/webhook", "U2e"},
	{"/adapters/provision/b2", "U2g"}, {"/data/prices", "U5"}, {"/adapters/check", "(後のイテレーション)"},
}

type counts struct{ notImpl, other, skip, pass int }

type event struct {
	Action, Package, Test, Output string
}

func wpOf(pkg string) string {
	for _, a := range assign {
		if strings.Contains(pkg, a.prefix) {
			return a.wp
		}
	}
	return ""
}

func main() {
	appFile := flag.String("app", "", "app/test/contract の結果(1 件 1 行の JSON: {\"wp\",\"status\",\"out\"})")
	flag.Parse()
	res := map[string]*counts{}
	add := func(wp, status, out string) {
		if wp == "" {
			return
		}
		c := res[wp]
		if c == nil {
			c = &counts{}
			res[wp] = c
		}
		switch {
		case status == "pass":
			c.pass++
		case status == "skip" && strings.Contains(out, markLater):
			c.skip++
		case status == "fail" && strings.Contains(out, markNotImpl):
			c.notImpl++
		case status == "fail":
			c.other++
		}
	}
	goTests(add)
	lintInstall(add)
	if *appFile != "" {
		appResults(*appFile, add)
	}
	var wps []string
	for w := range res {
		wps = append(wps, w)
	}
	sort.Strings(wps)
	for _, w := range wps {
		c := res[w]
		fmt.Printf("%-8s 未実装 %d / その他 %d / skip %d / 合格 %d\n", w, c.notImpl, c.other, c.skip, c.pass)
	}
}

func goTests(add func(wp, status, out string)) {
	cmd := exec.Command("go", "test", "-json", "-count=1", "./internal/...", "./adapters/...", "./data/...")
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "go test:", err)
		os.Exit(2)
	}
	outs := map[string]*strings.Builder{}
	testFailed := map[string]bool{}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e event
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		key := e.Package + "|" + e.Test
		switch e.Action {
		case "output":
			if outs[key] == nil {
				outs[key] = &strings.Builder{}
			}
			outs[key].WriteString(e.Output)
		case "pass", "fail", "skip":
			if e.Test == "" && e.Action != "fail" {
				continue
			}
			o := ""
			if b := outs[key]; b != nil {
				o = b.String()
			}
			if e.Test == "" { // パッケージ全体の失敗(ビルドエラーなど)は、テスト単位の失敗が無いときだけ数える
				if !testFailed[e.Package] {
					add(wpOf(e.Package), "fail", o)
				}
				continue
			}
			// make test-contract と同じく、internal/ の部品では TestContract だけを契約試験として数える(WP 自身の試験を混ぜない)
			if strings.Contains(e.Package, "/internal/") && !strings.HasPrefix(e.Test, "TestContract") {
				continue
			}
			if e.Action == "fail" {
				testFailed[e.Package] = true
			}
			add(wpOf(e.Package), e.Action, o)
		}
	}
	_ = cmd.Wait()
}

// lintInstall は docs/install.md の骨格の未実装の印による失敗を U4 の「未実装」に数える。
func lintInstall(add func(wp, status, out string)) {
	out, _ := exec.Command("go", "run", "./tools/lintinstall").CombinedOutput()
	st := "pass"
	if strings.Contains(string(out), markNotImpl) {
		st = "fail"
	} else if len(out) > 0 && strings.Contains(string(out), "lint-install: 不合格") {
		st = "fail"
	}
	add("U4", st, string(out))
}

func appResults(path string, add func(wp, status, out string)) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "app:", err)
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r struct{ WP, Status, Out string }
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			add(r.WP, r.Status, r.Out)
		}
	}
}
