package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 受け口の終了状態の写像(spec 3.2.2 の表)。list の 3・4・5・その他の 1〜123・127〜254 は対象 failed(end を出す)、126 と 255 は終了 1(end を出さない)。
func TestFilesListExitStatusMapping(t *testing.T) {
	for _, c := range []struct {
		rc   int
		code string
	}{{3, "NOT_FOUND"}, {4, "PERMISSION"}, {5, "EXIT_STATUS"}, {7, "EXIT_STATUS"}, {123, "EXIT_STATUS"}, {124, "EXIT_STATUS"}, {200, "EXIT_STATUS"}, {254, "EXIT_STATUS"}} {
		f := newFakeRecv(t)
		f.setList(nil, c.rc)
		rr := runAdapter(t, f.path(), request("t1", "/data", nil))
		if rr.code != 0 {
			t.Errorf("rc=%d: 終了 %d: %s", c.rc, rr.code, rr.stdout)
			continue
		}
		p := parseOutput(t, rr.stdout)
		if p.result.Status != "failed" || len(p.result.Errors) != 1 || p.result.Errors[0].Code != c.code || p.result.Errors[0].ItemRef != nil {
			t.Errorf("rc=%d: %+v", c.rc, p.result)
		}
		if string(p.raw["errors"]) != fmt.Sprintf(`[{"code":%q,"item_ref":null}]`, c.code) {
			t.Errorf("rc=%d: 対象全体は item_ref: null: %s", c.rc, p.raw["errors"])
		}
		if string(p.end.ExitStatus) != fmt.Sprint(c.rc) {
			t.Errorf("rc=%d: end.exit_status=%s", c.rc, p.end.ExitStatus)
		}
	}
}

func TestFilesExitDeniedAndUnreachable(t *testing.T) {
	for _, c := range []struct {
		rc           int
		code, detail string
	}{{126, "RECEIVER_TARGETS_MISMATCH", `"target_ids":["t1"]`}, {255, "STORAGE_UNREACHABLE", `"target":"source"`}} {
		f := newFakeRecv(t)
		f.setList([]listRec{fileRec("a", 1)}, c.rc)
		rr := runAdapter(t, f.path(), request("t1", "/data", nil))
		out := string(rr.stdout)
		if rr.code != 1 || !strings.Contains(out, `"code":"`+c.code+`"`) || !strings.Contains(out, c.detail) {
			t.Errorf("rc=%d: 終了 %d: %s", c.rc, rr.code, out)
		}
		if strings.Contains(out, `"type":"end"`) {
			t.Errorf("rc=%d: end を出さない", c.rc)
		}
		last := out[strings.LastIndex(strings.TrimRight(out, "\n"), "\n")+1:]
		if err := contract.Validate("adapter/acquire.json#/$defs/response", []byte(last)); err != nil {
			t.Errorf("rc=%d: 最終行が応答のスキーマに合わない: %v", c.rc, err)
		}
	}
}

// read-batch の終了状態: 5 は failed(読みかけの項目は item_end の status: error で閉じてから end)、126・255 は終了 1。
func TestFilesReadBatchExitStatus(t *testing.T) {
	f := newFakeRecv(t)
	f.setList([]listRec{fileRec("a", 3), fileRec("b", 3)}, 0)
	f.setBatch(1, map[string]string{"a": "aaa"}, []string{"a"}, nil, 5)
	rr := runAdapter(t, f.path(), request("t1", "/data", nil))
	p := parseOutput(t, rr.stdout)
	if rr.code != 0 || p.result.Status != "failed" || string(p.end.ExitStatus) != "5" {
		t.Errorf("5: 終了 %d %+v end=%s", rr.code, p.result, p.end.ExitStatus)
	}
	for _, c := range []struct {
		rc   int
		code string
	}{{126, "RECEIVER_TARGETS_MISMATCH"}, {255, "STORAGE_UNREACHABLE"}} {
		f := newFakeRecv(t)
		f.setList([]listRec{fileRec("a", 3), fileRec("b", 3)}, 0)
		f.setBatch(1, map[string]string{}, nil, nil, c.rc)
		rr := runAdapter(t, f.path(), request("t1", "/data", nil))
		if rr.code != 1 || !strings.Contains(string(rr.stdout), c.code) {
			t.Errorf("read-batch %d: 終了 %d: %s", c.rc, rr.code, rr.stdout)
		}
	}
}

// 途中で壊れた tar: 読みかけの file を item_end の status: error で閉じ、対象は failed。
func TestFilesTruncatedTar(t *testing.T) {
	f := newFakeRecv(t)
	f.setList([]listRec{fileRec("a", 3000)}, 0)
	f.setBatch(1, map[string]string{"a": strings.Repeat("x", 3000)}, []string{"a"}, nil, 0)
	b, _ := os.ReadFile(filepath.Join(f.dir, "rb.1"))
	f.put("rb.1", b[:700]) // 見出しと本体の途中で切る
	rr := runAdapter(t, f.path(), request("t1", "/data", nil))
	p := parseOutput(t, rr.stdout)
	if p.result.Status != "failed" {
		t.Fatalf("壊れた tar は failed: %+v", p.result)
	}
	var sawErr bool
	for _, fr := range p.frames {
		if fr.h.Type == "item_end" && fr.h.Status == "error" {
			sawErr = true
		}
	}
	if !sawErr {
		t.Error("読みかけの項目は item_end の status: error で閉じる")
	}
}

// 一覧の後に消えたパス(read-batch の tar に現れない)は NOT_FOUND。他は取れて partial。
func TestFilesNotFoundAfterList(t *testing.T) {
	f := newFakeRecv(t)
	f.setList([]listRec{fileRec("a", 1), fileRec("b", 1), fileRec("c", 1)}, 0)
	f.setBatch(1, map[string]string{"a": "A", "c": "C"}, []string{"a", "c"}, nil, 0)
	p := parseOutput(t, runAdapter(t, f.path(), request("t1", "/data", nil)).stdout)
	if p.result.Status != "partial" || len(p.result.Errors) != 1 || p.result.Errors[0].Code != "NOT_FOUND" || *p.result.Errors[0].PathB64 != b64([]byte("b")) {
		t.Errorf("結果: %+v", p.result)
	}
	if b := p.bodies("file"); b["a"] != "A" || b["c"] != "C" {
		t.Errorf("本体: %v", b)
	}
	if in := f.batchInput(1); len(in) != 3 || string(in[0]) != "a" || string(in[2]) != "c" {
		t.Errorf("read-batch に渡したパス一覧はソート順の 3 件: %q", in)
	}
}

// errors[] の件数の境界(ACQUIRE_ERRORS_MAX = 50): 50 件は全部載り、51 件は 1 件省かれ errors_omitted が 1、stats.errors は総件数。
func TestFilesErrorsBoundary(t *testing.T) {
	for _, c := range []struct{ n, omitted int }{{contract.AcquireErrorsMax, 0}, {contract.AcquireErrorsMax + 1, 1}} {
		f := newFakeRecv(t)
		var recs []listRec
		for i := 0; i < c.n; i++ {
			r := fileRec(fmt.Sprintf("f%03d", i), 1)
			r.readable = false
			recs = append(recs, r)
		}
		f.setList(recs, 0)
		p := parseOutput(t, runAdapter(t, f.path(), request("t1", "/data", nil)).stdout)
		st := p.result.Stats
		if st.Errors != int64(c.n) || st.ErrorsOmitted != int64(c.omitted) || p.result.Status != "partial" {
			t.Errorf("n=%d: stats=%+v status=%s", c.n, st, p.result.Status)
		}
		if got := len(p.result.Errors); got != c.n-c.omitted {
			t.Errorf("n=%d: errors[] の件数 %d(省かれるのは先頭から %d 件の後)", c.n, got, contract.AcquireErrorsMax)
		}
		if p.result.Errors[0].PathB64 == nil || *p.result.Errors[0].PathB64 != b64([]byte("f000")) {
			t.Errorf("n=%d: 先頭から載せる: %+v", c.n, p.result.Errors[0])
		}
	}
}

// 受け口の出力が契約に反する(並び順が逆)ときは INTERNAL(output)。
func TestFilesListOrderViolation(t *testing.T) {
	f := newFakeRecv(t)
	f.setList([]listRec{fileRec("b", 1), fileRec("a", 1)}, 0)
	rr := runAdapter(t, f.path(), request("t1", "/data", nil))
	if rr.code != 1 || !strings.Contains(string(rr.stdout), `"reason":"output"`) {
		t.Errorf("終了 %d: %s", rr.code, rr.stdout)
	}
}

// UTF-8 として正しくない名前は path_b64(file・errors[])。
func TestFilesInvalidUTF8Path(t *testing.T) {
	f := newFakeRecv(t)
	bad := fileRec("\xff\xfe", 1)
	unread := fileRec("\xff\xffz", 1)
	unread.readable = false
	f.setList([]listRec{bad, unread}, 0)
	f.setBatch(1, map[string]string{"\xff\xfe": "Z"}, []string{"\xff\xfe"}, nil, 0)
	p := parseOutput(t, runAdapter(t, f.path(), request("t1", "/data", nil)).stdout)
	var seen bool
	for _, fr := range p.frames {
		if fr.h.Type == "file" {
			seen = fr.h.PathB64 != nil && fr.h.Path == nil && *fr.h.PathB64 == b64([]byte("\xff\xfe"))
		}
	}
	if !seen || *p.result.Errors[0].PathB64 != b64([]byte("\xff\xffz")) {
		t.Errorf("path_b64: %+v %+v", p.frames, p.result.Errors)
	}
}
