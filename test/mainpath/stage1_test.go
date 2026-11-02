//go:build mainpath

package mainpath

// 主経路の第 1 段(作業指示書 6 章): backup → 保管先 → restore(その場)→ 照合。

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func emptyDir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "restored")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

func (w *world) mustRestore(label, backup string, snap []byte, exp map[string]want) {
	w.t.Helper()
	to := emptyDir(w.t)
	res, err := w.restoreTo(backup, snap, to)
	if err != nil {
		w.t.Fatalf("%s: Restore: %v", label, err)
	}
	w.t.Logf("%s: restore の結果 %s", label, res)
	// spec 3.1.2.4: 対象ごとに <to>/<target-id>/ の下に置く。
	if des, _ := os.ReadDir(to); len(des) != 1 || des[0].Name() != "t1" {
		w.t.Errorf("%s: 復元先の直下が <target-id> だけでない: %v", label, des)
	}
	compare(w.t, label, exp, filepath.Join(to, "t1"))
	w.verifyByCore(label, exp, filepath.Join(to, "t1"))
}

// verifyByCore は RestoreVerify を比較器として使う: 期待は元データから、実際は復元先から計算する(保存済みマニフェストは使わない)。
func (w *world) verifyByCore(label string, exp map[string]want, dest string) {
	t := w.t
	toIn := func(m map[string]want) string {
		var in contract.RestoreVerifyInput
		var ks []string
		for k, v := range m {
			if v.Type == "file" {
				ks = append(ks, k)
			}
		}
		sort.Strings(ks)
		for _, k := range ks {
			in.Files = append(in.Files, contract.RestoreVerifyFile{EntryRef: k, Size: m[k].Size, Hash: "blake3:" + m[k].Hash})
		}
		b, _ := json.Marshal(in)
		return string(b)
	}
	call := func(act map[string]want) map[string]any {
		r, err := w.rst.RestoreVerify(w.ctx, contract.Call{Words: []string{"restore-verify"}, Args: map[string][]string{
			contract.ArgRestoreVerifyExpected: {toIn(exp)}, contract.ArgRestoreVerifyActual: {toIn(act)}}})
		if err != nil {
			t.Fatalf("%s: RestoreVerify: %v", label, err)
		}
		var m map[string]any
		_ = json.Unmarshal(r.Result, &m)
		return m
	}
	if m := call(scan(t, dest)); m["ok"] != true {
		t.Errorf("%s: RestoreVerify が不一致: %v", label, m)
	}
	bad := scan(t, dest)
	v := bad["data/small.bin"]
	v.Hash = strings.Repeat("0", 64)
	bad["data/small.bin"] = v
	if m := call(bad); m["ok"] != false {
		t.Errorf("%s: RestoreVerify がハッシュの違いを見逃した: %v", label, m)
	}
}

func TestStage1BackupRestoreMatchesSource(t *testing.T) {
	w := newWorld(t)
	g := w.backup("")
	t.Logf("取得: files=%d bytes=%d chunks_new=%d bytes_new=%d entries=%d keytable=%d", g.Ing.Targets[0].Files, g.Ing.Targets[0].Bytes, g.Ing.ChunksNew, g.Ing.BytesNew, g.Ing.EntriesCount, g.Ing.KeyTableCount)
	nfiles := 0
	for _, e := range g.Exp {
		if e.Type == "file" {
			nfiles++
		}
	}
	if int(g.Ing.Targets[0].Files) != nfiles {
		t.Errorf("取得した files=%d 元データのファイル数 %d", g.Ing.Targets[0].Files, nfiles)
	}
	if g.Ing.ChunksNew < 5 {
		t.Errorf("大きなファイルが複数チャンクになっていない(chunks_new=%d)", g.Ing.ChunksNew)
	}
	w.mustRestore("gen1(明示の ID)", g.MID, g.SnapKey, g.Exp)
	// latest でも同じ世代が選ばれる(健常性 pass かつ全対象 ok)。
	w.mustRestore("gen1(latest)", "latest", g.SnapKey, g.Exp)
}

func TestStage1SecondBackupDedupAndGenerations(t *testing.T) {
	w := newWorld(t)
	g1 := w.backup("")
	// 一部だけ変更する: 小さなファイルを書き換え、新しいファイルと削除を足す。大きなファイルは触らない。
	mt := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	writeFile(t, filepath.Join(w.src, "data", "small.bin"), pseudo(11, 6000), 0o644, mt)
	writeFile(t, filepath.Join(w.src, "docs", "new.txt"), []byte("2 回目で足したファイル\n"), 0o644, mt)
	if err := os.Remove(filepath.Join(w.src, "docs", "empty.dat")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // 保管先の LastModified は秒の精度
	g2 := w.backup(g1.MID)
	t.Logf("gen1: chunks_new=%d bytes_new=%d / gen2: chunks_new=%d bytes_new=%d chunks_referenced=%d", g1.Ing.ChunksNew, g1.Ing.BytesNew, g2.Ing.ChunksNew, g2.Ing.BytesNew, g2.Ing.ChunksReferenced)
	if g2.Ing.BytesNew >= bigSize/4 {
		t.Errorf("重複排除が効いていない: 2 回目の bytes_new=%d(大きなファイル %d は変更なし)", g2.Ing.BytesNew, bigSize)
	}
	if g2.Ing.ChunksNew >= g1.Ing.ChunksNew {
		t.Errorf("2 回目の chunks_new=%d は 1 回目 %d より少ないはず", g2.Ing.ChunksNew, g1.Ing.ChunksNew)
	}
	w.mustRestore("gen2(明示の ID)", g2.MID, g2.SnapKey, g2.Exp)
	w.mustRestore("gen2(latest)", "latest", g2.SnapKey, g2.Exp)
	w.mustRestore("gen1(当時の内容)", g1.MID, g1.SnapKey, g1.Exp)
}

// chunkKeyOf はデータのチャンクの 1 つ(保管先で最大のもの)のキーと版 ID。
func (w *world) biggestChunk() (string, string) {
	vs, err := w.put.ListVersions(w.ctx, "chunks/")
	if err != nil {
		w.t.Fatal(err)
	}
	var k, v string
	var sz int64 = -1
	for _, x := range vs {
		if x.Size > sz {
			k, v, sz = x.Key, x.VersionID, x.Size
		}
	}
	return k, v
}

func TestStage1RecordedVersionIsRead(t *testing.T) {
	w := newWorld(t)
	g := w.backup("")
	key, ver := w.biggestChunk()
	// 同じキーに別の内容の版を重ねる(書き込み鍵で積まれた偽の版を想定)。最新はこちらになる。
	res, err := w.put.Put(w.ctx, key, bytes.NewReader([]byte("attacker-controlled bytes")), contract.RetentionDaily)
	if err != nil {
		t.Fatalf("差し替えの版を置けない: %v", err)
	}
	if res.VersionID == ver {
		t.Fatalf("版 ID が変わらない")
	}
	w.mustRestore("差し替え後の gen1", g.MID, g.SnapKey, g.Exp)

	// 記録した版が無いとき(エントリのチャンクの版 ID を存在しない値にした世代)は失敗する。最新の版にフォールバックしない。
	bad, badKey := w.forgeManifest(g, func(m map[string]any) {
		ch := m["entries"].(map[string]any)["chunks"].([]any)
		ch[0].(map[string]any)["version_id"] = "00000000-0000-0000-0000-000000000000"
	})
	to := emptyDir(t)
	if _, err := w.restoreTo(bad, badKey, to); err == nil {
		t.Errorf("記録した版が無いのに restore が成功した")
	} else {
		expectCode(t, "エントリのチャンクの版が無い", err, contract.CodeDecryptFailed, "missing")
	}
	if des, _ := os.ReadDir(to); len(des) > 0 {
		t.Errorf("失敗した restore が復元先に %d 件を書いた", len(des))
	}

	// エントリの範囲が指すデータのチャンクの版が無いとき(範囲の版 ID を存在しない値にした世代)も失敗する。spec 3.5: RESTORE_MISMATCH の hash_mismatch[].reason: missing。
	bad2, bad2Key := w.forgeEntries(g)
	_, err = w.restoreTo(bad2, bad2Key, emptyDir(t))
	expectCode(t, "データのチャンクの版が無い", err, contract.CodeRestoreMismatch, "missing")
}

// expectCode は誤りの原因コードと、detail の文字列に reason が含まれることを確かめる。
func expectCode(t *testing.T, label string, err error, code contract.Code, reason string) {
	t.Helper()
	var ce *contract.Error
	if !errors.As(err, &ce) {
		t.Errorf("%s: 型付きの誤りでない: %v", label, err)
		return
	}
	if ce.Code != code {
		t.Errorf("%s: 原因コード %s 期待 %s(detail %s)", label, ce.Code, code, ce.Detail)
		return
	}
	if !strings.Contains(string(ce.Detail), reason) {
		t.Errorf("%s: detail に %q が無い: %s", label, reason, ce.Detail)
	}
	t.Logf("%s: %s %s", label, ce.Code, ce.Detail)
}

func TestStage1WrongSnapshotKeyCannotDecrypt(t *testing.T) {
	w := newWorld(t)
	g := w.backup("")
	other, _, _, err := w.keyS.NewSnapshotKey(w.ctx, "m-"+strings.Repeat("ab", 16))
	if err != nil {
		t.Fatal(err)
	}
	to := emptyDir(t)
	_, err = w.restoreTo(g.MID, other, to)
	if err == nil {
		t.Fatalf("別のスナップショット鍵で restore が成功した")
	}
	var ce *contract.Error
	if !errors.As(err, &ce) {
		t.Errorf("型付きの誤りでない: %v", err)
	} else {
		t.Logf("別のスナップショット鍵の誤り: %s", ce.Code)
	}
	if des, _ := os.ReadDir(to); len(des) > 0 {
		t.Errorf("失敗した restore が復元先に書いた")
	}
}

func TestStage1RetentionExtendAndPrune(t *testing.T) {
	w := newWorld(t)
	g := w.backup("")
	before, _ := w.put.ListVersions(w.ctx, "")
	ex, err := w.ret.Extend(w.ctx, "A")
	if err != nil {
		t.Fatalf("Extend(A): %v", err)
	}
	t.Logf("Extend(A): extended=%d failed=%v min_retain_until=%v", ex.Extended, ex.Failed, ex.MinRetainUntil)
	if len(ex.Failed) > 0 {
		t.Errorf("Extend(A) の失敗: %+v", ex.Failed)
	}
	exB, err := w.ret.Extend(w.ctx, "B")
	if err != nil {
		t.Fatalf("Extend(B): %v", err)
	}
	t.Logf("Extend(B): extended=%d failed=%v", exB.Extended, exB.Failed)
	pr, err := w.ret.Prune(w.ctx)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	t.Logf("Prune: %+v", pr)
	if pr.Deleted != 0 {
		t.Errorf("期限前なのに Prune が %d 件を消した", pr.Deleted)
	}
	after, _ := w.put.ListVersions(w.ctx, "")
	if len(after) != len(before) {
		t.Errorf("Prune の前後で版の数が変わった: %d → %d", len(before), len(after))
	}
	vr, err := w.ret.Verify(w.ctx, w.recorded())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	t.Logf("Verify: %+v", vr)
	if len(vr.Missing)+len(vr.RetentionShort)+len(vr.UnexpectedVersions) > 0 {
		t.Errorf("記録どおりに書いたのに Verify が差を出した: %+v", vr)
	}
	w.mustRestore("Extend・Prune の後の gen1", g.MID, g.SnapKey, g.Exp)
}

// recorded は cli が Verify に渡す記録: チャンク(索引)・objects[]・snapshots[](マニフェスト)。
func (w *world) recorded() []contract.VersionRef {
	recs, err := w.idx.Records(w.ctx)
	if err != nil {
		w.t.Fatal(err)
	}
	pt := func(s string) time.Time { x, _ := time.Parse(time.RFC3339, s); return x }
	var objs []struct {
		Key, VersionID, RetainUntil string
	}
	raw, _ := w.st.Get(w.ctx, "objects")
	var rawObjs []map[string]any
	_ = json.Unmarshal(raw, &rawObjs)
	for _, o := range rawObjs {
		recs = append(recs, contract.VersionRef{Key: o["key"].(string), VersionID: o["version_id"].(string), RetainUntil: pt(o["retain_until"].(string))})
	}
	_ = objs
	raw, _ = w.st.Get(w.ctx, "snapshots")
	var snaps []map[string]any
	_ = json.Unmarshal(raw, &snaps)
	for _, s := range snaps {
		recs = append(recs, contract.VersionRef{Key: "manifests/" + s["ref"].(string), VersionID: s["version_id"].(string), RetainUntil: pt(s["retain_until"].(string))})
	}
	return recs
}
