//go:build mainpath

package mainpath

// 第 3 段: 同じ id で根を変え、新しい根に同一メタデータ・異内容のファイルを置いたとき、次の backup が新しい内容を取得する
// (作業指示書 5.18 の完了条件 5。5.17 の完了条件 15。D-0118 の 3)。期待値は元サーバーの根から台本が計算した行で、保存済みのマニフェストは使わない。
// 2 つの根のファイルは種別・モード・サイズ・更新時刻・相対パスが同じで、内容のハッシュだけが違う。

import (
	"fmt"
	"strings"
)

const (
	rootA = "/srv/rootchange-a"
	rootB = "/srv/rootchange-b"
)

// plantRoots は 2 つの根に、同一メタデータ・異内容のファイルを置く(内容は同じ長さ)。
func (w *s2) plantRoots() {
	script := ""
	for _, r := range []struct{ dir, tag string }{{rootA, "A"}, {rootB, "B"}} {
		script += fmt.Sprintf(`rm -rf %[1]s && mkdir -p %[1]s/sub
printf 'content-%[2]s-0123456789\n' > %[1]s/f.txt
printf 'second-%[2]s-abcdefghij\n' > %[1]s/sub/g.txt
chmod 0644 %[1]s/f.txt %[1]s/sub/g.txt
touch -d '2026-10-01 08:00:00 UTC' %[1]s/f.txt %[1]s/sub/g.txt
touch -d '2026-10-01 09:00:00 UTC' %[1]s/sub %[1]s
`, r.dir, r.tag)
	}
	w.src.root(script)
}

// changeTargetRoot は t1 の根を files に変える source-change(対象 id は t1 のまま)を提案 → 承認 → 適用する。
func (w *s2) changeTargetRoot(files string) {
	src := map[string]any{"host": accEnv(w.t, "SITATE_ACCEPT_SOURCE_HOST"), "admin_user": adminUser(w.t),
		"targets":  []any{map[string]any{"id": "t1", "files": files}},
		"declined": []any{map[string]any{"id": "d1", "postgresql_cluster": 5432}}}
	id := w.proposeKind("propose source-change("+files+")", "", "source-change", "--source", compact(src))
	w.approveKind("source-change("+files+")", id, nil)
	w.applyKind("source-change("+files+")", id, w.sourceAdmin(), "--source-admin-ref", "admin", "--secrets-stdin")
}

// backupAndRestoreMatches は backup を 1 回取り、その世代を dest に復元して、元の根の行(台本が元データから計算)と比べる。
func (w *s2) backupAndRestoreMatches(name, root string) string {
	t := w.t
	want := w.src.root(listing(root))
	o := w.g.call("", "backup").ok(t, "backup("+name+")")
	ref, _ := o.resultMap(t)["snapshot_ref"].(string)
	if ref == "" {
		t.Fatalf("backup(%s) に snapshot_ref が無い: %s", name, o.Env.Result)
	}
	to := fmt.Sprintf("%s/rootchange-%s", destRestoreRoot, name)
	w.dst.root("install -d -m 0700 -o sitate -g sitate " + to)
	w.restoreOnDest(ref, to, w.snapshotKey(ref)).ok(t, "dest の restore("+name+")")
	got := w.dst.root(listing(to + "/t1"))
	if d := diffLines(want, got); len(d) > 0 {
		t.Errorf("根 %s の世代 %s: 元データと復元先が一致しない:\n%s", root, ref, strings.Join(d, "\n"))
	} else {
		t.Logf("根 %s の世代 %s: 元データと復元先の種別・モード・サイズ・更新時刻・SHA-256 が一致(%d 行)", root, ref, strings.Count(got, "\n"))
	}
	return want
}

func (w *s2) rootChange() {
	t := w.t
	w.plantRoots()
	a, b := w.src.root(listing(rootA)), w.src.root(listing(rootB))
	meta := func(s string) string { // 内容のハッシュの行を除いた、メタデータの行
		var out []string
		for _, l := range strings.Split(s, "\n") {
			if strings.Count(l, "\t") >= 4 {
				out = append(out, l)
			}
		}
		return strings.Join(out, "\n")
	}
	if meta(a) != meta(b) || a == b {
		t.Fatalf("台本の前提: 2 つの根はメタデータが同じで内容だけが違う\nA:\n%s\nB:\n%s", a, b)
	}
	w.changeTargetRoot(rootA)
	w.backupAndRestoreMatches("a", rootA)
	w.changeTargetRoot(rootB) // 同じ id t1 の根だけを変える
	wantB := w.backupAndRestoreMatches("b", rootB)
	if wantB == a {
		t.Errorf("台本の誤り: 期待値が古い内容と同じ")
	}
}
