package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeebo/blake3"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func h3(b []byte) string {
	s := blake3.Sum256(b)
	return "blake3:" + hex.EncodeToString(s[:])
}

func itemRefs(r contract.AcquireResult) []string {
	var out []string
	for _, e := range r.Errors {
		if e.ItemRef == nil {
			out = append(out, "null:"+e.Code)
		} else {
			out = append(out, *e.ItemRef+":"+e.Code)
		}
	}
	return out
}

func exitOf(t *testing.T, h contract.StreamHeader) string {
	t.Helper()
	return string(h.ExitStatus)
}

// 完了条件: globals を本体より先に送り、dump フレームの format が globals | custom になる。全項目が成功なら ok。
func TestBothItemsOK(t *testing.T) {
	f := newFakeRecv(t)
	g, c := []byte("CREATE ROLE x;\n"), append([]byte("PGDMP"), bytes.Repeat([]byte{7}, 100)...)
	f.set("dump-pg-globals", g, 0)
	f.set("dump-pg", c, 0)
	r := runAdapter(t, f.path(), request("t2"))
	if r.code != 0 {
		t.Fatalf("終了 %d\n%s", r.code, r.stdout)
	}
	p := parseOutput(t, r.stdout)
	d := p.dumps(t)
	if len(d) != 2 || d[0].format != contract.DumpFormatGlobals || d[1].format != contract.DumpFormatCustom {
		t.Fatalf("dump の順と format: %+v", d)
	}
	if !bytes.Equal(d[0].body, g) || !bytes.Equal(d[1].body, c) {
		t.Errorf("本体が受け口の標準出力と違う")
	}
	for i, body := range [][]byte{g, c} {
		if d[i].end.Hash != h3(body) || *d[i].end.Bytes != int64(len(body)) || d[i].end.Status != "ok" || exitOf(t, d[i].end) != "0" {
			t.Errorf("item_end[%d]: %+v", i, d[i].end)
		}
	}
	if got := f.calls(); len(got) != 2 || got[0] != "dump-pg-globals t2" || got[1] != "dump-pg t2" {
		t.Errorf("受け口の呼び出しは項目ごとに 1 回、globals が先: %v", got)
	}
	if p.result.Status != "ok" || len(p.result.Errors) != 0 || p.result.Stats.Errors != 0 || exitOf(t, p.end) != "0" {
		t.Errorf("結果: %s", p.line)
	}
}

// 完了条件: ダンプの途中失敗を失敗として返す。pg_dump が途中まで出力して非 0 で終わる。
func TestDumpFailsMidway(t *testing.T) {
	f := newFakeRecv(t)
	f.set("dump-pg-globals", []byte("g"), 0)
	f.set("dump-pg", []byte("PGDMP-half"), 1)
	r := runAdapter(t, f.path(), request("t1"))
	if r.code != 0 {
		t.Fatalf("終了 %d\n%s", r.code, r.stdout)
	}
	p := parseOutput(t, r.stdout)
	d := p.dumps(t)
	if len(d) != 2 || d[1].end.Status != "error" || exitOf(t, d[1].end) != "1" || string(d[1].body) != "PGDMP-half" {
		t.Errorf("custom の item_end: %+v", d)
	}
	if p.result.Status != "partial" || strings.Join(itemRefs(p.result), ",") != "d:t1:custom:EXIT_STATUS" || p.result.Stats.Errors != 1 {
		t.Errorf("結果: %s", p.line)
	}
	if exitOf(t, p.end) != "1" {
		t.Errorf("end.exit_status は最後の呼び出しの値: %s", exitOf(t, p.end))
	}
}

// 完了条件: globals の失敗は partial で、errors[].item_ref は d:<target-id>:globals。本体(custom)はなお送る。
func TestGlobalsFailureIsPartial(t *testing.T) {
	f := newFakeRecv(t)
	f.set("dump-pg-globals", nil, 4)
	f.set("dump-pg", []byte("PGDMP"), 0)
	r := runAdapter(t, f.path(), request("t12"))
	p := parseOutput(t, r.stdout)
	d := p.dumps(t)
	if r.code != 0 || len(d) != 2 || d[0].format != "globals" || d[0].end.Status != "error" || exitOf(t, d[0].end) != "4" || *d[0].end.Bytes != 0 {
		t.Fatalf("globals の item_end: code=%d %+v", r.code, d)
	}
	if d[1].end.Status != "ok" {
		t.Errorf("custom は送る: %+v", d[1].end)
	}
	if p.result.Status != "partial" || strings.Join(itemRefs(p.result), ",") != "d:t12:globals:EXIT_STATUS" {
		t.Errorf("結果: %s", p.line)
	}
	if exitOf(t, p.end) != "0" {
		t.Errorf("end.exit_status: %s", exitOf(t, p.end))
	}
}

// 全項目のダンプが失敗なら failed。125(起動できない・シグナル)と 124(道具の版が無い)も項目の失敗。
func TestAllItemsFailIsFailed(t *testing.T) {
	f := newFakeRecv(t)
	f.set("dump-pg-globals", nil, 124)
	f.set("dump-pg", nil, 125)
	r := runAdapter(t, f.path(), request("t1"))
	p := parseOutput(t, r.stdout)
	if r.code != 0 || p.result.Status != "failed" || strings.Join(itemRefs(p.result), ",") != "d:t1:globals:EXIT_STATUS,d:t1:custom:EXIT_STATUS" {
		t.Errorf("code=%d 結果: %s", r.code, p.line)
	}
	if exitOf(t, p.end) != "125" {
		t.Errorf("end.exit_status: %s", exitOf(t, p.end))
	}
}

// 127〜254(受け口が無いか受け口の外で止まった)は対象の failed、item_ref: null。それ以上は呼ばない。
func TestReceiverMissingFailsTarget(t *testing.T) {
	for _, rc := range []int{127, 200, 254} {
		f := newFakeRecv(t)
		f.set("dump-pg-globals", nil, rc)
		r := runAdapter(t, f.path(), request("t1"))
		p := parseOutput(t, r.stdout)
		if r.code != 0 || p.result.Status != "failed" || strings.Join(itemRefs(p.result), ",") != "null:EXIT_STATUS" {
			t.Errorf("rc=%d code=%d 結果: %s", rc, r.code, p.line)
		}
		if len(f.calls()) != 1 {
			t.Errorf("rc=%d 後続を呼んだ: %v", rc, f.calls())
		}
	}
}

// 126 は RECEIVER_TARGETS_MISMATCH、255 は STORAGE_UNREACHABLE(終了 1、end を出さない)。
func TestMismatchAndUnreachable(t *testing.T) {
	cases := []struct {
		rc   int
		code string
		det  string
	}{
		{126, "RECEIVER_TARGETS_MISMATCH", `"target_ids":["t3"]`},
		{255, "STORAGE_UNREACHABLE", `"reason":"connect"`},
	}
	for _, c := range cases {
		for _, op := range []string{"dump-pg-globals", "dump-pg"} {
			f := newFakeRecv(t)
			f.set("dump-pg-globals", []byte("g"), 0)
			f.set(op, []byte("x"), c.rc)
			r := runAdapter(t, f.path(), request("t3"))
			last := r.stdout[bytes.LastIndexByte(bytes.TrimRight(r.stdout, "\n"), '\n')+1:]
			if r.code != 1 || !strings.Contains(string(last), `"code":"`+c.code+`"`) || !strings.Contains(string(last), c.det) {
				t.Errorf("%s %d: code=%d\n%s", op, c.rc, r.code, r.stdout)
			}
			if bytes.Contains(r.stdout, []byte(`"type":"end"`)) {
				t.Errorf("%s %d: end を出した", op, c.rc)
			}
		}
	}
}

// 本体は StreamFragmentMax 以下の断片に分けて送る(ためない)。
func TestLargeBodyIsFragmented(t *testing.T) {
	f := newFakeRecv(t)
	body := bytes.Repeat([]byte("0123456789abcdef"), (contract.StreamFragmentMax*2+1000)/16)
	f.set("dump-pg-globals", []byte("g"), 0)
	f.set("dump-pg", body, 0)
	r := runAdapter(t, f.path(), request("t1"))
	p := parseOutput(t, r.stdout)
	n := 0
	for _, fr := range p.frames {
		if fr.h.Type == contract.FrameTypeData {
			n++
			if *fr.h.Size < 1 || *fr.h.Size > contract.StreamFragmentMax {
				t.Errorf("断片の大きさ %d", *fr.h.Size)
			}
		}
	}
	d := p.dumps(t)
	if n < 4 || !bytes.Equal(d[1].body, body) || d[1].end.Hash != h3(body) {
		t.Errorf("断片 %d 個、本体の一致 %v", n, bytes.Equal(d[1].body, body))
	}
}

// 空の本体の項目は data を置かず item_end だけ(空の本体も BLAKE3 のハッシュ)。
func TestEmptyBody(t *testing.T) {
	f := newFakeRecv(t)
	f.set("dump-pg-globals", nil, 0)
	f.set("dump-pg", nil, 0)
	p := parseOutput(t, runAdapter(t, f.path(), request("t1")).stdout)
	for _, d := range p.dumps(t) {
		if d.end.Hash != h3(nil) || *d.end.Bytes != 0 || d.end.Status != "ok" {
			t.Errorf("%+v", d)
		}
	}
}

// item_ref は固定値と対象 id だけで作る(ロール名・DB 名を使わない)。標準エラーの文言も結果に入れない。
func TestItemRefHasOnlyFixedParts(t *testing.T) {
	f := newFakeRecv(t)
	f.set("dump-pg-globals", nil, 1)
	f.set("dump-pg", nil, 1)
	r := runAdapter(t, f.path(), request("t7"))
	p := parseOutput(t, r.stdout)
	for _, e := range p.result.Errors {
		if e.PathB64 != nil || e.ItemRef == nil || !strings.HasPrefix(*e.ItemRef, "d:t7:") {
			t.Errorf("要素 %+v", e)
		}
	}
	var raw struct {
		Errors []map[string]any `json:"errors"`
	}
	_ = json.Unmarshal(p.line, &raw)
	for _, e := range raw.Errors {
		if _, has := e["path"]; has || len(e) != 2 {
			t.Errorf("要素の欄: %v", e)
		}
	}
}

// 要求の不正と SITATE_RECV が無いときは終了 1 の INTERNAL(end を出さない)。
func TestBadRequests(t *testing.T) {
	f := newFakeRecv(t)
	bad := request("t1")
	bad["target"].(map[string]any)["kind"] = "files"
	badID := request("x1")
	for name, c := range map[string]struct {
		recv string
		req  any
	}{"kind": {f.path(), bad}, "id": {f.path(), badID}, "no-recv": {"", request("t1")}} {
		r := runAdapter(t, c.recv, c.req)
		if r.code != 1 || !strings.Contains(string(r.stdout), `"code":"INTERNAL"`) || !strings.Contains(string(r.stdout), `"reason":"request"`) || bytes.Contains(r.stdout, []byte(`"type":"end"`)) {
			t.Errorf("%s: code=%d %s", name, r.code, r.stdout)
		}
	}
	if len(f.calls()) != 0 {
		t.Errorf("要求が不正でも受け口を呼んだ: %v", f.calls())
	}
}
