package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 読み取り中に変化したファイル: 最初の読みの変化の一覧に載ったファイルは同じ実行の中で 1 回だけ読み直し、file-retry で送り直す。
// 読み直しで落ち着けば item_end は ok、なお変化していれば status: changed・警告 CHANGED_DURING_READ・対象は ok のまま。
func TestFilesRetry(t *testing.T) {
	f := newFakeRecv(t)
	f.setList([]listRec{fileRec("a", 3), fileRec("b", 3)}, 0)
	f.setBatch(1, map[string]string{"a": "aa1", "b": "bb1"}, []string{"a", "b"}, []string{"a", "b"}, 0)
	f.setBatch(2, map[string]string{"a": "aa2"}, []string{"a"}, nil, 0)           // a は落ち着いた
	f.setBatch(3, map[string]string{"b": "bb2"}, []string{"b"}, []string{"b"}, 0) // b はなお変化している
	p := parseOutput(t, runAdapter(t, f.path(), request("t1", "/data", nil)).stdout)
	if p.result.Status != "ok" || p.result.Stats.ChangedDuringRead != 1 || p.result.Stats.Errors != 0 {
		t.Fatalf("対象は ok のまま・changed_during_read は 1: %+v", p.result)
	}
	if len(p.result.Warnings) != 1 || p.result.Warnings[0].Code != "CHANGED_DURING_READ" || *p.result.Warnings[0].PathB64 != b64([]byte("b")) {
		t.Errorf("warnings: %+v", p.result.Warnings)
	}
	retry := p.bodies("file-retry")
	if retry["a"] != "aa2" || retry["b"] != "bb2" {
		t.Errorf("file-retry の本体: %v", retry)
	}
	if first := p.bodies("file"); first["a"] != "aa1" || first["b"] != "bb1" {
		t.Errorf("先の file は取り消さない: %v", first)
	}
	st := map[string]string{}
	var cur string
	for _, fr := range p.frames {
		switch fr.h.Type {
		case "file-retry":
			cur = pathOf(fr.h)
		case "item_end":
			if cur != "" {
				st[cur] = fr.h.Status
			}
			cur = ""
		}
	}
	if st["a"] != "ok" || st["b"] != "changed" {
		t.Errorf("file-retry の item_end の status: %v", st)
	}
	if n := len(f.batchInput(2)) + len(f.batchInput(3)); n != 2 || f.batchInput(4) != nil {
		t.Errorf("読み直しは 1 ファイル 1 回: %q %q", f.batchInput(2), f.batchInput(3))
	}
	// 先の file が先、file-retry が後(同じ path の先の file の無い file-retry は契約違反)
	order := p.typeOrder()
	if strings.Index(order, "file-retry") < strings.LastIndex(order, "file,") {
		t.Errorf("file-retry は先の file の後: %s", order)
	}
}

func (p parsed) typeOrder() string {
	var b strings.Builder
	for _, f := range p.frames {
		if f.h.Type == "file" || f.h.Type == "file-retry" {
			b.WriteString(f.h.Type + ",")
		}
	}
	return b.String()
}

// warnings[] の件数の境界: 50 件は全部載り、51 件は 1 件省かれて warnings_omitted が 1(ACQUIRE_ERRORS_MAX)。
func TestFilesWarningsBoundary(t *testing.T) {
	for _, c := range []struct{ n, omitted int }{{contract.AcquireErrorsMax, 0}, {contract.AcquireErrorsMax + 1, 1}} {
		f := newFakeRecv(t)
		var recs []listRec
		files := map[string]string{}
		var names []string
		for i := 0; i < c.n; i++ {
			name := fmt.Sprintf("f%03d", i)
			recs = append(recs, fileRec(name, 1))
			files[name] = "x"
			names = append(names, name)
		}
		f.setList(recs, 0)
		f.setBatch(1, files, names, names, 0)
		for i, name := range names {
			f.setBatch(i+2, map[string]string{name: "y"}, []string{name}, []string{name}, 0)
		}
		p := parseOutput(t, runAdapter(t, f.path(), request("t1", "/data", nil)).stdout)
		st := p.result.Stats
		if got := len(p.result.Warnings); got != c.n-c.omitted || st.WarningsOmitted != int64(c.omitted) || st.ChangedDuringRead != int64(c.n) {
			t.Errorf("n=%d: warnings %d 件・stats=%+v", c.n, got, st)
		}
		if p.result.Status != "ok" || st.Errors != 0 {
			t.Errorf("n=%d: 警告だけなら対象は ok: %+v", c.n, p.result)
		}
	}
}

// 先に file を送っていない要素は読み直さない: 1 回目の tar に現れず変化の一覧に載ったファイルは、file-retry を出さず NOT_FOUND だけ。
func TestFilesNoRetryWithoutFile(t *testing.T) {
	f := newFakeRecv(t)
	f.setList([]listRec{fileRec("a", 1)}, 0)
	f.setBatch(1, map[string]string{}, nil, []string{"a"}, 0)
	f.setBatch(2, map[string]string{"a": "A"}, []string{"a"}, nil, 0)
	p := parseOutput(t, runAdapter(t, f.path(), request("t1", "/data", nil)).stdout)
	if len(p.bodies("file-retry")) != 0 || f.batchInput(2) != nil {
		t.Errorf("file-retry を出した: %v", p.bodies("file-retry"))
	}
	if p.result.Status != "partial" || len(p.result.Errors) != 1 || p.result.Errors[0].Code != "NOT_FOUND" {
		t.Errorf("結果: %+v", p.result)
	}
}

// 変化の一覧の区切りが無い read-batch の出力は契約違反(INTERNAL の output)。
func TestFilesMissingChangedMarker(t *testing.T) {
	f := newFakeRecv(t)
	f.setList([]listRec{fileRec("a", 1)}, 0)
	f.setBatch(1, map[string]string{"a": "A"}, []string{"a"}, nil, 0)
	b, _ := os.ReadFile(filepath.Join(f.dir, "rb.1"))
	f.put("rb.1", b[:bytes.Index(b, []byte(contract.ReadBatchChangedMarker))])
	rr := runAdapter(t, f.path(), request("t1", "/data", nil))
	if rr.code != 1 || !strings.Contains(string(rr.stdout), `"reason":"output"`) {
		t.Errorf("終了 %d: %s", rr.code, rr.stdout)
	}
}

// 途中で切れた list は終了状態で分類する(255 は届かなかったとして STORAGE_UNREACHABLE。end なし)。
func TestFilesListCutOff(t *testing.T) {
	f := newFakeRecv(t)
	f.setList([]listRec{fileRec("a", 1)}, 255)
	b, _ := os.ReadFile(filepath.Join(f.dir, "list"))
	f.put("list", b[:len(b)-5])
	rr := runAdapter(t, f.path(), request("t1", "/data", nil))
	if rr.code != 1 || !strings.Contains(string(rr.stdout), "STORAGE_UNREACHABLE") || strings.Contains(string(rr.stdout), `"type":"end"`) {
		t.Errorf("終了 %d: %s", rr.code, rr.stdout)
	}
}
