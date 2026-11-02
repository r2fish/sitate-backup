package acquire

// 取得の入口(Ingest・Run)の中身の試験(作業指示書 5.4 の完了条件。契約試験に置けなかった Run の正常系)。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

// entriesOf はエントリのチャンクを読み、path ごとのエントリにする。
func entriesOf(t *testing.T, e *ingestEnv, r IngestResult) map[string]entry {
	t.Helper()
	m := map[string]entry{}
	sc := bufio.NewScanner(bytes.NewReader(e.concat(t, r.EntriesChunks)))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var en entry
		if err := json.Unmarshal(sc.Bytes(), &en); err != nil {
			t.Fatalf("エントリを読めない: %v: %s", err, sc.Text())
		}
		m[string(en.Path)] = en
	}
	return m
}

// content は鍵表の鍵でエントリの ranges をたどって、ファイルの内容を作る。
func content(t *testing.T, e *ingestEnv, r IngestResult, en entry) []byte {
	t.Helper()
	keys := map[string]ChunkInfo{}
	sc := bufio.NewScanner(bytes.NewReader(e.concat(t, r.KeyTableChunks)))
	for sc.Scan() {
		var row struct{ ID, V, K string }
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		keys[row.ID] = ChunkInfo{ID: row.ID, VersionID: row.V, Key: row.K}
	}
	var out []byte
	for _, rg := range *en.Ranges {
		k, ok := keys[rg.ID]
		if !ok {
			t.Fatalf("鍵表に %s が無い", rg.ID)
		}
		plain := e.readChunk(t, k.ID, k.VersionID, k.Key)
		out = append(out, plain[rg.Off:rg.Off+rg.Len]...)
	}
	return out
}

func TestIngestStoresFilesAndKeys(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	a1, a2 := []byte("hello, backup world"), []byte("second file body is longer than the first one")
	fb := newFrames().dir(".").file("a.txt", a1).symlink("ln", "a.txt").file("sub/b.txt", a2).file("empty", nil)
	e.script(fb.end(false), okResult("ok"))
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Targets) != 1 || r.Targets[0].Status != "ok" || r.Targets[0].Files != 3 || r.Targets[0].Bytes != int64(len(a1)+len(a2)) {
		t.Fatalf("対象の結果: %+v", r.Targets)
	}
	en := entriesOf(t, e, r)
	if len(en) != 5 || r.EntriesCount != 5 {
		t.Fatalf("エントリの数: %d / %d", len(en), r.EntriesCount)
	}
	for path, want := range map[string][]byte{"t1/a.txt": a1, "t1/sub/b.txt": a2, "t1/empty": nil} {
		got := content(t, e, r, en[path])
		if !bytes.Equal(got, want) {
			t.Errorf("%s の内容を ranges から戻せない: %q", path, got)
		}
		if en[path].Hash == "" || *en[path].Size != int64(len(want)) {
			t.Errorf("%s の hash・size: %+v", path, en[path])
		}
	}
	if l := en["t1/ln"]; l.Type != "symlink" || string(l.LinkTo) != "a.txt" {
		t.Errorf("symlink: %+v", l)
	}
	if r.ChunkKeysMissing != 0 || r.ChunksReferenced == 0 || r.ChunksNew == 0 || r.BytesNew == 0 {
		t.Errorf("統計: %+v", r)
	}
	if !fileExists(e.indexFile("t1")) {
		t.Error("取れた対象の index/files/t1 を書く")
	}
}

// 2 回目は前回の索引を previous_index で渡し、unchanged のエントリは前回の写しになる。データのチャンクは重複排除で送らない。
func TestIngestSecondRunUsesPreviousIndex(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	body := []byte("persistent content")
	e.script(newFrames().dir(".").file("keep.txt", body).end(false), okResult("ok"))
	r1, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	var req1 contract.AcquireRequest
	_ = json.Unmarshal(e.ad.Calls[0].Req, &req1)
	if req1.PreviousIndex != nil {
		t.Errorf("初回の previous_index は null: %v", *req1.PreviousIndex)
	}
	e.script(newFrames().dir(".").unchanged("keep.txt", len(body)).end(false), okResult("ok"))
	r2, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	var req2 contract.AcquireRequest
	_ = json.Unmarshal(e.ad.Calls[1].Req, &req2)
	if req2.PreviousIndex == nil || !strings.HasSuffix(*req2.PreviousIndex, "t1.prev.idx") {
		t.Errorf("2 回目は previous_index を渡す: %v", req2.PreviousIndex)
	}
	if got := content(t, e, r2, entriesOf(t, e, r2)["t1/keep.txt"]); !bytes.Equal(got, body) {
		t.Errorf("unchanged のエントリが前回の範囲を指す: %q", got)
	}
	if r2.ManifestID == r1.ManifestID {
		t.Error("manifest_id は実行ごとに新しい")
	}
	if r2.ChunkKeysMissing != 0 {
		t.Errorf("前の世代のチャンクの鍵は索引から引ける: %d", r2.ChunkKeysMissing)
	}
}

func TestIngestFileRetryReplacesEarlierRead(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	first, second := []byte("first read, torn"), []byte("second read, whole")
	e.script(newFrames().dir(".").file("f", first).retry("f", second, "changed").end(false), okResult("ok"))
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	en := entriesOf(t, e, r)["t1/f"]
	if got := content(t, e, r, en); !bytes.Equal(got, second) {
		t.Errorf("file-retry の内容に置き換わる: %q", got)
	}
	if len(en.Flags) != 1 || en.Flags[0] != "changed_during_read" || r.Targets[0].ChangedDuringRead != 1 {
		t.Errorf("changed_during_read の印: %+v %+v", en.Flags, r.Targets[0])
	}
	var sum int64
	for _, rg := range *en.Ranges {
		sum += rg.Len
	}
	if sum != int64(len(second)) || *en.Size != int64(len(second)) {
		t.Errorf("先の読みの範囲を持たない: ranges %d size %d", sum, *en.Size)
	}
}

// end の hash が合わないストリームは、その対象を failed にして index/files を更新しない。すべて failed なら ALL_TARGETS_FAILED。
func TestIngestEndMismatchFailsTarget(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.script(newFrames().dir(".").file("a", []byte("x")).end(true), okResult("ok"))
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if r.Targets[0].Status != "failed" || r.Targets[0].ErrorCode == "" {
		t.Errorf("end の不一致は failed: %+v", r.Targets[0])
	}
	if fileExists(e.indexFile("t1")) {
		t.Error("failed の対象の index/files を更新しない")
	}
	if _, err := r.reply(); err == nil {
		t.Error("すべて failed なら ALL_TARGETS_FAILED")
	} else if ce, ok := err.(*contract.Error); !ok || ce.Code != contract.CodeAllTargetsFailed {
		t.Errorf("原因コード: %v", err)
	}
}

func TestIngestContractViolations(t *testing.T) {
	for name, build := range map[string]func() []contract.Frame{
		"item_end の hash が本体と違う": func() []contract.Frame {
			fb := newFrames().dir(".")
			fb.add(map[string]any{"type": "file", "path": "a", "size": 1, "mode": 420, "uid": 0, "gid": 0, "mtime_ns": 1}, nil)
			fb.add(map[string]any{"type": "data", "size": 1}, []byte("x"))
			fb.add(map[string]any{"type": "item_end", "bytes": 1, "hash": "blake3:" + strings.Repeat("0", 64), "status": "ok", "exit_status": nil}, nil)
			return fb.end(false)
		},
		"パスに .. を含む": func() []contract.Frame { return newFrames().file("../x", []byte("x")).end(false) },
		"先の file が無い file-retry": func() []contract.Frame {
			return newFrames().retry("nofile", []byte("x"), "ok").end(false)
		},
		"end が無い": func() []contract.Frame { fr := newFrames().dir(".").end(false); return fr[:len(fr)-1] },
	} {
		t.Run(name, func(t *testing.T) {
			e := newIngestEnv(t, ingestDef)
			e.script(build(), okResult("ok"))
			r, err := e.run(t)
			if err != nil {
				t.Fatal(err)
			}
			if r.Targets[0].Status != "failed" || fileExists(e.indexFile("t1")) {
				t.Errorf("契約違反の対象は failed で索引を更新しない: %+v", r.Targets[0])
			}
		})
	}
}

// 取得の結果の errors[] の path_b64 は、そのパスのエントリの ref(item_ref)に置き換わり、パスが出ない。省いた件数は写す。
func TestIngestErrorsBecomeItemRefs(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	res := rawJSON(map[string]any{"contract": "sitate.acquire/v1", "status": "partial", "stats": map[string]any{"files": 1, "bytes": 1, "errors": 3,
		"errors_omitted": 2, "warnings_omitted": 4, "changed_during_read": 0},
		"errors": []map[string]any{{"code": "PERMISSION", "path_b64": b64("secret/name.txt")}}, "warnings": []any{}})
	e.script(newFrames().dir(".").file("a", []byte("x")).end(false), res)
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	tg := r.Targets[0]
	want := e.a.KeyS.EntryRef("t1/secret/name.txt")
	if tg.Status != "partial" || len(tg.Errors) != 1 || tg.Errors[0].Code != "PERMISSION" || tg.Errors[0].ItemRef == nil || *tg.Errors[0].ItemRef != want {
		t.Errorf("errors[]: %+v (want ref %s)", tg.Errors, want)
	}
	if tg.ErrorsOmitted != 2 || tg.WarningsOmitted != 4 {
		t.Errorf("省いた件数: %+v", tg)
	}
	rep, err := r.reply()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rep.Result), "secret") || strings.Contains(string(rep.Result), b64("secret")) {
		t.Errorf("結果にパスが出た: %s", rep.Result)
	}
	var out struct{ Health string }
	_ = json.Unmarshal(rep.Result, &out)
	if out.Health != "incomplete" {
		t.Errorf("partial の対象があれば incomplete: %s", rep.Result)
	}
}

// 1 つの対象の到達不能は、その対象だけを failed にして次の対象へ進む。
func TestIngestUnreachableTargetDoesNotStopRun(t *testing.T) {
	def := `{"version":1,"source":{"host":"127.0.0.1","targets":[{"id":"t1","files":"/a"},{"id":"t2","files":"/b"}]}}`
	e := newIngestEnv(t, def)
	e.ad.Script("acquire", fake.Step{Outcome: fake.OutcomeError, ErrCode: "STORAGE_UNREACHABLE"})
	e.script(newFrames().dir(".").file("x", []byte("y")).end(false), okResult("ok"))
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Targets) != 2 || r.Targets[0].Status != "failed" || r.Targets[0].ErrorCode != "STORAGE_UNREACHABLE" || r.Targets[1].Status != "ok" {
		t.Fatalf("対象ごとの結果: %+v", r.Targets)
	}
	if fileExists(e.indexFile("t1")) || !fileExists(e.indexFile("t2")) {
		t.Error("切れた対象の索引は更新しない")
	}
	rep, err := r.reply()
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate("cli/results.json#/$defs/backup", rep.Result); err != nil {
		t.Errorf("backup の結果のスキーマ: %v", err)
	}
}

func TestIngestAbortedWritesNothing(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.script(newFrames().dir(".").file("a", []byte("x")).end(false), okResult("ok"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.a.Ingest(ctx, contract.Call{Words: []string{"backup"}, RunID: testRunID})
	if ce, ok := err.(*contract.Error); !ok || ce.Code != contract.CodeRunAborted {
		t.Fatalf("RUN_ABORTED: %v", err)
	}
	if v, _ := e.a.Store.ListVersions(context.Background(), ""); len(v) != 0 || fileExists(e.indexFile("t1")) || len(e.ad.Calls) != 0 {
		t.Error("取り消しで保管先・索引・取得へ進んだ")
	}
}

// 同じ内容を 2 回の実行で送ると、データのチャンクは重複排除で送り直さない(エントリと鍵表のチャンクだけが新しい)。
func TestIngestDedupSkipsKnownChunks(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	body := bytes.Repeat([]byte("dedup me "), 50)
	e.script(newFrames().dir(".").file("same", body).end(false), okResult("ok"))
	r1, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	e.script(newFrames().dir(".").file("same", body).end(false), okResult("ok"))
	r2, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if r1.ChunksNew < 3 { // データ 1 + エントリ 1 + 鍵表 1 以上
		t.Fatalf("初回に送ったチャンク: %d", r1.ChunksNew)
	}
	if r2.ChunksNew >= r1.ChunksNew {
		t.Errorf("2 回目はデータのチャンクを送らない: 初回 %d 2 回目 %d", r1.ChunksNew, r2.ChunksNew)
	}
	if got := content(t, e, r2, entriesOf(t, e, r2)["t1/same"]); !bytes.Equal(got, body) {
		t.Errorf("重複排除した範囲から戻せる")
	}
}

// イテレーション 1 には健常性チェックが無い: checks[] は空で、取得アダプタ以外(チェックの容器)を呼ばない。
func TestIngestNoChecks(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.script(newFrames().dir(".").file("a", []byte("x")).end(false), okResult("ok"))
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	rep, _ := r.reply()
	var out struct{ Checks []any }
	_ = json.Unmarshal(rep.Result, &out)
	if out.Checks == nil || len(out.Checks) != 0 {
		t.Errorf("checks[] は空の配列: %s", rep.Result)
	}
	for _, c := range e.ad.Calls {
		if c.Kind != "acquire" {
			t.Errorf("取得アダプタ以外を起動した: %s", c.Kind)
		}
	}
}

// 複数のチャンクにまたがる大きなファイルと、その後ろの小さなファイル: ranges のオフセット・長さがチャンクの境界で合い、内容を戻せる。
func TestIngestRangesAcrossChunks(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	big := make([]byte, 3*contract.ChunkMin+12345)
	rand.New(rand.NewSource(1)).Read(big)
	small := []byte("small file after the big one")
	fb := newFrames()
	fb.frag = contract.StreamFragmentMax
	fb.dir(".").file("big.bin", big).file("small.txt", small)
	e.script(fb.end(false), okResult("ok"))
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	en := entriesOf(t, e, r)
	if n := len(*en["t1/big.bin"].Ranges); n < 3 {
		t.Fatalf("大きなファイルは複数のチャンクにまたがる: %d", n)
	}
	for path, want := range map[string][]byte{"t1/big.bin": big, "t1/small.txt": small} {
		if got := content(t, e, r, en[path]); !bytes.Equal(got, want) {
			t.Errorf("%s の内容を ranges から戻せない", path)
		}
	}
}
