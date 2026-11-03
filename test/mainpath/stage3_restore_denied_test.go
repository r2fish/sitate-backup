//go:build mainpath

package mainpath

// 第 3 段: gate を通る restore の --to の字面(ssh://../keys/pending)を、核が普通のパスとして読んで状態ディレクトリへ書かない
// (まとめレビュー C1)。実物の gate 経由で送り、拒否されることと、状態ディレクトリの keys/ にも cwd の周りにも何も作られないことを確かめる。

import "strings"

func (w *s2) restorePlantDenied() {
	t := w.t
	snap := func() string {
		return w.bak.root(`cd ` + stateDir + ` && (find . -maxdepth 3 \( -path ./keys/pending -o -path ./pending \) 2>/dev/null; ls -A keys; find keys -type f | sort | xargs -r sha256sum) | sha256sum`)
	}
	before := snap()
	for _, to := range []string{"ssh://../keys/pending", "source:../keys/pending", "restore-dest:../keys/pending"} {
		o := w.g.call("", "restore", "--to", to, "--endpoint", w.s3.endpoint, "--bucket", w.s3.bucket,
			"--prefix", "sitate/", "--backup", "latest", "--snapshot-key-stdin")
		if o.Env.OK || o.code() != "ARG_INVALID" {
			t.Errorf("gate 経由の restore --to %s: 期待は ARG_INVALID、実際は ok=%v %s(終了 %d)detail=%.200s", to, o.Env.OK, o.code(), o.Code, o.Env.Error.Detail)
			continue
		}
		t.Logf("gate 経由の restore --to %s: ARG_INVALID(終了 %d)", to, o.Code)
	}
	if after := snap(); after != before {
		t.Errorf("拒否された restore の後で状態ディレクトリの keys/ が変わった")
	}
	if got := strings.TrimSpace(w.bak.root(`test -e ` + stateDir + `/keys/pending && echo exists || echo none`)); got != "none" {
		t.Errorf("keys/pending ができた: %s", got)
	}
}
