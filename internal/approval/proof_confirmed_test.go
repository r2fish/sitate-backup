package approval

// spec 3.1.2.7 status.notify.proof_confirmed: seal の承認で確認語が一致したら true。

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func proofConfirmedOf(t *testing.T, e *env) any {
	t.Helper()
	raw, _ := e.st.Get(ctx, "notify")
	var n map[string]json.RawMessage
	_ = json.Unmarshal(raw, &n)
	var v any
	if b, ok := n["proof_confirmed"]; ok {
		_ = json.Unmarshal(b, &v)
	}
	return v
}

func TestSealApprovalSetsProofConfirmed(t *testing.T) {
	e := newEnv(t, true)
	salt := []byte("0123456789abcdef")
	e.st.Items["notify"] = mustJSON(map[string]any{"last_test_at": "2026-11-02T00:00:00Z", "test_proofs": []proofRow{{To: "a@x", Salt: base64.StdEncoding.EncodeToString(salt), Hash: wordHash(salt, "one two three")}}})
	e.st.Items["sealed"] = json.RawMessage(`false`)
	h := "sha256:" + strings.Repeat("2", 64)
	e.a.State = &candState{StateFiles: e.st, cand: []byte(`{"notify":{"to":["a@x"]}}`), candHash: h, defHash: h}
	e.withDef(`{"notify":{"to":["a@x"]}}`)
	p := e.propose("seal", map[string][]string{"file": {"-"}})
	want(t, "違う語", approveWithProof(e, p, `{"a@x":"one two four"}`), "NOTIFY_PROOF_MISMATCH", "")
	if v := proofConfirmedOf(t, e); v == true {
		t.Fatalf("一致しない語で true になった")
	}
	if err := approveWithProof(e, p, `{"a@x":"One  Two three"}`); err != nil {
		t.Fatal(err)
	}
	if v := proofConfirmedOf(t, e); v != true {
		t.Fatalf("seal の承認で確認語が一致したら true のはず: %v", v)
	}
	raw, _ := e.st.Get(ctx, "notify")
	if !strings.Contains(string(raw), "last_test_at") {
		t.Errorf("notify の他の項目を消した: %s", raw)
	}
}
