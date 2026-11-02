package acquire

// Ingest と周辺の結線の試験: 容器(Wrap・刈り取り)、取得の前の自己診断との突き合わせ、from= の必須、作業ディレクトリの掃除、索引の破損、再構成の境界。

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// wiredEnv は pull 接続(偽の受け口、固定値、pull 用鍵)まで結線した取得の入口の環境。
func wiredEnv(t *testing.T, selftest string) (*ingestEnv, *fakeSource) {
	t.Helper()
	e := newIngestEnv(t, ingestDef)
	src := startSource22(t, replySelftest(selftest))
	st := e.a.State.(*fake.StateFiles)
	st.Items["host_keys"] = hostKeysItem("127.0.0.1", src.FP)
	st.Items["receiver_hash"] = json.RawMessage(`"` + checkHash + `"`)
	e.a.SSH = routeSSH{}
	e.a.Secrets = &fake.SecretStore{M: map[string]json.RawMessage{contract.SourcePullKeyRef: pullKeySecret(t, newAdminKey(t))}}
	return e, src
}

func TestIngestSelftestMismatchSkipsTarget(t *testing.T) {
	e, _ := wiredEnv(t, strings.Replace(selftestDoc(1, true, checkHash, "ok"), "/srv/app", "/srv/old", 1))
	e.script(newFrames().dir(".").end(false), okResult("ok"))
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.ad.Calls) != 0 || len(r.TargetsMismatch) != 1 || r.TargetsMismatch[0] != "t1" || r.Targets[0].Status != "failed" || r.Targets[0].ErrorCode != "RECEIVER_TARGETS_MISMATCH" {
		t.Errorf("合わない対象は取らず failed: calls %d %+v %+v", len(e.ad.Calls), r.TargetsMismatch, r.Targets)
	}
	if _, err := r.reply(); err == nil {
		t.Error("すべて failed なら ALL_TARGETS_FAILED")
	}
}

func TestIngestSelftestUnreachableAndFailed(t *testing.T) {
	e, src := wiredEnv(t, selftestDoc(1, true, checkHash, "ok"))
	src.ln.Close()
	e.script(newFrames().dir(".").end(false), okResult("ok"))
	_, err := e.run(t)
	harness.ExpectCode(t, err, contract.CodeStorageUnreachable)
	e, _ = wiredEnv(t, selftestDoc(2, true, checkHash, "ok"))
	_, err = e.run(t)
	harness.ExpectCode(t, err, contract.CodeReceiverSelftestFailed)
	if len(e.ad.Calls) != 0 {
		t.Error("取得を始めた")
	}
}

// fakePodman は引数を記録する偽の podman(ps は容器 1 つを返す)。
func fakePodman(t *testing.T) (path, log string) {
	dir := t.TempDir()
	log = filepath.Join(dir, "log")
	path = filepath.Join(dir, "podman")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\ncase \"$1\" in ps) echo cid1;; esac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, log
}

func TestIngestWithContainerWrapsAndReaps(t *testing.T) {
	e, _ := wiredEnv(t, selftestDoc(1, true, checkHash, "ok"))
	pod, log := fakePodman(t)
	e.a.Container = &Container{Podman: pod, RecvExe: "/opt/sitate/sitate", Home: "/home/sitate", RuntimeDir: "/run/user/1000"}
	e.script(newFrames().dir(".").file("a", []byte("x")).end(false), okResult("ok"))
	if _, err := e.run(t); err != nil {
		t.Fatal(err)
	}
	wrap := e.ad.Calls[0].RC.Wrap
	if wrap == nil {
		t.Fatal("rc.Wrap が置かれていない(隔離なしで起動される)")
	}
	argv, err := wrap("/opt/adapters/files", []string{"PATH=/usr/bin", "LC_ALL=C"})
	if err != nil {
		t.Fatal(err)
	}
	j := strings.Join(argv, " ")
	for _, want := range []string{"env HOME=/home/sitate XDG_RUNTIME_DIR=/run/user/1000 " + pod + " run", "--network none", "--read-only", "--cap-drop all", "--label sitate-run=" + testRunID} {
		if !strings.Contains(j, want) {
			t.Errorf("引数列に %q が無い: %s", want, j)
		}
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "label=sitate-run="+testRunID) || !strings.Contains(string(b), "rm -f cid1") {
		t.Errorf("取得の後に run_id のラベルの容器を片付ける: %s", b)
	}
}

// 容器を使う設定で pull 用鍵が無ければ、直接起動に落とさず誤りにする。
func TestIngestWithContainerNeedsPullKey(t *testing.T) {
	e, _ := wiredEnv(t, selftestDoc(1, true, checkHash, "ok"))
	e.a.Container = &Container{RecvExe: "/opt/sitate/sitate"}
	e.a.Secrets = &fake.SecretStore{M: map[string]json.RawMessage{}}
	e.script(newFrames().dir(".").end(false), okResult("ok"))
	if _, err := e.run(t); err == nil || len(e.ad.Calls) != 0 {
		t.Errorf("誤りで止まり、取得アダプタを起動しない: %v", err)
	}
}

// from= が空のまま source-setup の台本を流さない。
func TestEnrollRequiresFrom(t *testing.T) {
	w := newEnrollWorld(t)
	a, st := newSrcAcquire(enrollItems(map[string]json.RawMessage{"source_seen": seenItem("127.0.0.1", w.src.FP)}))
	a.Secrets = &fake.SecretStore{M: map[string]json.RawMessage{}}
	a.From = ""
	_, err := a.SourceEnroll(ctx, sourceCall(enrollWords, w.src.URL("root"), newAdminKey(t)))
	harness.ExpectCode(t, err, contract.CodeInternal)
	if ex := w.src.Execs(); len(ex) != 1 { // 調査 (db_only) だけ
		t.Errorf("台本を流した: %d 回", len(ex))
	}
	if len(st.Items["host_keys"]) != 0 {
		t.Error("記録を書いた")
	}
}

func TestIngestCleansStaleWorkDir(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	stale := filepath.Join(e.a.Dir, "work", "r-20260101T000000Z-dead")
	_ = os.MkdirAll(stale, 0o755)
	_ = os.WriteFile(filepath.Join(stale, "t1.prev.idx"), []byte("plain paths"), 0o644)
	e.script(newFrames().dir(".").end(false), okResult("ok"))
	if _, err := e.run(t); err != nil {
		t.Fatal(err)
	}
	if fileExists(stale) {
		t.Error("異常終了の後に残った作業ディレクトリ(平文の前回の索引)を消さない")
	}
}

func TestIngestWarningsAreCopied(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	res := rawJSON(map[string]any{"contract": "sitate.acquire/v1", "status": "ok", "stats": map[string]any{"files": 1, "bytes": 1, "errors": 0, "errors_omitted": 0,
		"warnings_omitted": 0, "changed_during_read": 0}, "errors": []any{}, "warnings": []map[string]any{{"code": "CHANGED_DURING_READ", "path_b64": b64("a")}}})
	tg := targetOf(t, e, newFrames().dir(".").file("a", []byte("x")).end(false), res)
	if len(tg.Warnings) != 1 || tg.Warnings[0].Code != "CHANGED_DURING_READ" || tg.Warnings[0].ItemRef == nil {
		t.Errorf("warnings[] を写す: %+v", tg.Warnings)
	}
}

// 途中の記録の破損は書きかけではない: 索引を開けないと知らせ、黙って空として読まず、追記もしない。末尾だけなら捨てる。
func TestIndexMidCorruptionIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	ix := newIndex(dir)
	for _, id := range []string{idA, idB} {
		if err := ix.Reserve(ctx, ref(id)); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(dir, "index", "chunks")
	b, _ := os.ReadFile(p)
	n := int(binary.BigEndian.Uint32(b[:4]))
	b[4+n/2] ^= 0xff // 先頭の記録の本体を壊す(後ろに記録が続く)
	_ = os.WriteFile(p, b, 0o600)
	ix2 := newIndex(dir)
	_, err := ix2.Lookup(ctx, idA)
	var iu *IndexUnreadable
	if !errors.As(err, &iu) {
		t.Fatalf("索引を開けない誤り: %v", err)
	}
	if err := ix2.Reserve(ctx, ref("5555555555555555555555555555555555555555555555555555555555555555")); err == nil {
		t.Error("開けない索引へ追記した")
	}
	if after, _ := os.ReadFile(p); len(after) != len(b) {
		t.Errorf("索引のファイルが変わった: %d → %d", len(b), len(after))
	}
}

// 再構成: 最後の世代でその対象が failed なら index/files を作らない。取り消されたら古い索引を残す。
func TestIndexRebuildTargetStatusAndCancel(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.script(newFrames().dir(".").file("a", []byte("x")).end(false), okResult("ok"))
	r1, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	ix := e.a.Index.(*Index)
	ix.Store = e.a.Store
	_ = os.Remove(e.indexFile("t1"))
	body := manifestBodyOf(r1, "2026-11-02T00:00:00Z", ingestDef)
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	m["targets"] = []map[string]any{{"id": "t1", "status": "failed"}}
	failed, _ := json.Marshal(m)
	if err := ix.Rebuild(ctx, [][]byte{failed}, []byte(ingestDef)); err != nil {
		t.Fatal(err)
	}
	if fileExists(e.indexFile("t1")) {
		t.Error("failed の世代から index/files を作った")
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	if err := ix.Rebuild(c, [][]byte{body}, []byte(ingestDef)); err == nil {
		t.Error("取り消された再構成が成功した")
	}
	if fileExists(e.indexFile("t1")) {
		t.Error("取り消された再構成が index/files を置き換えた")
	}
}

// run_id が分からない掃除は、ラベル付きの停止済みだけを 1 回の ps で探す(ラベルの無い容器・動いている容器を巻き込まない)。
func TestRunCleanupWithoutInterruptedQueriesLabeledStoppedOnly(t *testing.T) {
	pod, log := fakePodman(t)
	if _, _, err := (&RunCleanup{Podman: pod}).Cleanup(context.Background(), "r-own", ""); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "--filter label=sitate-run --filter status=exited --filter status=created") || !strings.Contains(string(b), "--filter label=sitate-run=r-own") {
		t.Errorf("ps の問い合わせ: %s", b)
	}
}
