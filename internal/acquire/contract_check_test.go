package acquire

// source check の正常系の契約試験(spec 3.1.2 `source check`、3.3 の「`selftest` の出力」「`selftest[]` の項目」)。
// 判定する内容: 受け口(`sitate-ro` の pull 用鍵、固定したホスト鍵)へ selftest を実行し、その材料から selftest[] の項目(from_restricted・single_key_line・receiver_hash・
// targets_match)を ok / code / target_id / unreadable[] の値で返す。外れても誤りでなく結果に載せる(単独の source check)。読み取りのみ(状態ファイルを変えない)。
// 受け口の survey の応答の形は契約に固定が無いため、uncovered[] の中身は判定しない(報告)。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

const checkHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// startSource22 は pull 用の接続(source.host、ポート 22)の先に偽の元サーバーを置く。
func startSource22(t *testing.T, reply func(execRec) (string, int)) *fakeSource {
	t.Helper()
	s := startSource(t, reply)
	srcMu.Lock()
	srcByPt[22] = s
	srcMu.Unlock()
	t.Cleanup(func() { srcMu.Lock(); delete(srcByPt, 22); srcMu.Unlock() })
	return s
}

func selftestDoc(lines int, match bool, hash, rootState string) string {
	b, _ := json.Marshal(map[string]any{"format": "sitate.recv-selftest/v1", "version": "0.1.0", "receiver_hash": hash,
		"authorized_keys": map[string]any{"lines": lines, "match": match},
		"targets":         []any{map[string]any{"id": "t1", "kind": "files", "root": "/srv/app", "root_state": rootState}}})
	return string(b)
}

func checkAcquire(t *testing.T, src *fakeSource) (*Acquire, *fake.StateFiles) {
	t.Helper()
	a, st := newSrcAcquire(map[string]json.RawMessage{
		"sealed": sealedItem(true), "host_keys": hostKeysItem("127.0.0.1", src.FP), "receiver_hash": json.RawMessage(`"` + checkHash + `"`)})
	a.Secrets = &fake.SecretStore{M: map[string]json.RawMessage{contract.SourcePullKeyRef: pullKeySecret(t, newAdminKey(t))}}
	return a, st
}

func replySelftest(doc string) func(execRec) (string, int) {
	return func(r execRec) (string, int) {
		if strings.Contains(r.Cmd, "selftest") {
			return doc, 0
		}
		if strings.Contains(r.Cmd, "survey") {
			return `{"format":"sitate.recv-survey/v1","mounts":[],"clusters":[]}`, 0
		}
		return "{}", 0
	}
}

type stItem struct {
	Name     string   `json:"name"`
	TargetID *string  `json:"target_id"`
	OK       bool     `json:"ok"`
	Code     *string  `json:"code"`
	Unread   []string `json:"unreadable"`
}

func runCheck(t *testing.T, doc string) (map[string]stItem, *fakeSource, contract.Reply, map[string]json.RawMessage, map[string]json.RawMessage) {
	t.Helper()
	src := startSource22(t, nil)
	src.Reply = replySelftest(doc)
	a, st := checkAcquire(t, src)
	before := map[string]json.RawMessage{}
	for k, v := range st.Items {
		before[k] = v
	}
	r, err := a.SourceCheck(ctx, contract.Call{Words: []string{"source", "check"}, RunID: testRunID})
	harness.Shape(t, []string{"source", "check"}, r, err)
	var res struct {
		Selftest []stItem `json:"selftest"`
	}
	harness.Result(t, r, &res)
	m := map[string]stItem{}
	for _, i := range res.Selftest {
		m[i.Name] = i
	}
	return m, src, r, before, st.Items
}

func TestContractAcquireSourceCheckSelftest(t *testing.T) {
	_, perr := shapeAcquire().SourceCheck(ctx, contract.Call{Words: []string{"source", "check"}})
	harness.Implemented(t, perr)
	m, src, r, before, after := runCheck(t, selftestDoc(1, true, checkHash, "ok"))
	for _, n := range []string{"from_restricted", "single_key_line", "receiver_hash", "targets_match"} {
		it, ok := m[n]
		if !ok || !it.OK || it.Code != nil {
			t.Errorf("%s: %+v (ok で code は null)", n, it)
		}
	}
	if it := m["targets_match"]; it.TargetID == nil || *it.TargetID != "t1" {
		t.Errorf("targets_match の target_id は t1: %+v", it)
	}
	if it := m["from_restricted"]; it.TargetID != nil {
		t.Errorf("from_restricted の target_id は null: %+v", it)
	}
	if len(src.Execs()) == 0 || src.Execs()[0].User != "sitate-ro" || !strings.Contains(src.Execs()[0].Cmd, "selftest") {
		t.Errorf("sitate-ro で selftest を実行していない: %+v", src.Execs())
	}
	for _, k := range []string{"uncovered", "actionable", "new_since_last"} {
		if v := jsonField(t, r.Result, k); v == nil {
			t.Errorf("%s が配列でない", k)
		}
	}
	for k, v := range after {
		if string(before[k]) != string(v) && k != "source_seen" {
			t.Errorf("読み取りのみなのに %s を書いた", k)
		}
	}
}

func jsonField(t *testing.T, raw json.RawMessage, k string) any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m[k]
}

func TestContractAcquireSourceCheckFailures(t *testing.T) {
	_, perr := shapeAcquire().SourceCheck(ctx, contract.Call{Words: []string{"source", "check"}})
	harness.Implemented(t, perr)
	selfFail, tgtFail := string(contract.CodeReceiverSelftestFailed), string(contract.CodeReceiverTargetsMismatch)
	cases := []struct {
		name, doc, item, code string
	}{
		{"鍵の行が 2 つ", selftestDoc(2, true, checkHash, "ok"), "single_key_line", selfFail},
		{"行が置いた行と違う", selftestDoc(1, false, checkHash, "ok"), "from_restricted", selfFail},
		{"受け口のハッシュが違う", selftestDoc(1, true, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "ok"), "receiver_hash", selfFail},
		{"根が読めない", selftestDoc(1, true, checkHash, "unreadable"), "targets_match", tgtFail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _, _, _ := runCheck(t, c.doc)
			for n, it := range m {
				if n == c.item {
					if it.OK || it.Code == nil || *it.Code != c.code {
						t.Errorf("%s: %+v (ok: false, code: %s)", n, it, c.code)
					}
				} else if !it.OK {
					t.Errorf("%s: 別の項目 %s が落ちた", c.item, n)
				}
			}
			if _, ok := m[c.item]; !ok {
				t.Errorf("項目 %s が無い", c.item)
			}
			if c.item == "targets_match" {
				if it := m[c.item]; it.TargetID == nil || *it.TargetID != "t1" || len(it.Unread) != 1 || it.Unread[0] != "/srv/app" {
					t.Errorf("targets_match は target_id t1 と unreadable[/srv/app]: %+v", it)
				}
			}
		})
	}
}

// 固定したホスト鍵と見えた指紋が違えば、受け口を実行せずに SOURCE_HOSTKEY_MISMATCH。
func TestContractAcquireSourceCheckHostKey(t *testing.T) {
	_, perr := shapeAcquire().SourceCheck(ctx, contract.Call{Words: []string{"source", "check"}})
	harness.Implemented(t, perr)
	src := startSource22(t, replySelftest(selftestDoc(1, true, checkHash, "ok")))
	a, _ := checkAcquire(t, src)
	a.State = &srcState{StateFiles: &fake.StateFiles{Def: []byte(testDef), Items: map[string]json.RawMessage{"sealed": sealedItem(true), "host_keys": hostKeysItem("127.0.0.1", fpA)}}, cand: []byte(testDef)}
	_, err := a.SourceCheck(ctx, contract.Call{Words: []string{"source", "check"}, RunID: testRunID})
	harness.ExpectCode(t, err, contract.CodeSourceHostkeyMismatch)
	if len(src.Execs()) != 0 {
		t.Errorf("指紋が違うのに実行した: %d", len(src.Execs()))
	}
}

var _ = context.Background
