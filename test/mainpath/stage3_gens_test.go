//go:build mainpath

package mainpath

// 第 3 段: 世代ごとの別のサーバーへの復元の照合。元データと復元結果を同じ計算(種別・モード・サイズ・更新時刻・リンク先・SHA-256)で比べる。
// 期待値は各 backup の直後に台本が元データから計算した行で、保存済みのマニフェストは使わない。

import (
	"fmt"
	"strings"
)

func diffLines(want, got string) []string {
	if want == got {
		return nil
	}
	in := func(s string) map[string]bool {
		m := map[string]bool{}
		for _, x := range strings.Split(s, "\n") {
			m[x] = true
		}
		return m
	}
	wm, gm := in(want), in(got)
	var d []string
	for _, x := range strings.Split(want, "\n") {
		if !gm[x] {
			d = append(d, "元にあり復元に無い/違う: "+x)
		}
	}
	for _, x := range strings.Split(got, "\n") {
		if !wm[x] {
			d = append(d, "復元にあり元に無い/違う: "+x)
		}
	}
	return d
}

func (w *s2) restoreEachGeneration(gens []genRec) {
	t := w.t
	w.installCore()
	for i, g := range gens {
		to := fmt.Sprintf("%s/gen%d", destRestoreRoot, i)
		w.dst.root("install -d -m 0700 -o sitate -g sitate " + to)
		o := w.restoreOnDest(g.ref, to, w.snapshotKey(g.ref)).ok(t, "dest の restore("+g.name+")")
		t.Logf("restore(%s %s): %.300s", g.name, g.ref, o.Env.Result)
		got := w.dst.root(listing(to + "/t1"))
		if d := diffLines(g.want, got); len(d) > 0 {
			if len(d) > 20 {
				d = d[:20]
			}
			t.Errorf("%s(%s): 元データと復元先が一致しない(%d 行):\n%s", g.name, g.ref, len(d), strings.Join(d, "\n"))
			continue
		}
		t.Logf("%s(%s): 元データと復元先の種別・モード・サイズ・更新時刻・リンク先・SHA-256 が一致(照合の行 %d)", g.name, g.ref, strings.Count(got, "\n"))
	}
	// 世代の間で元データが実際に違っていた(照合が世代を区別できている)。
	for i := 1; i < len(gens); i++ {
		if gens[i].want == gens[i-1].want {
			t.Errorf("世代 %s と %s の元データの照合の行が同じ(変更が入っていない)", gens[i-1].name, gens[i].name)
		}
	}
}
