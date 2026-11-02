package approval

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func want(t *testing.T, name string, err error, code contract.Code, reason string) {
	t.Helper()
	if codeOf(err) != code || (reason != "" && reasonOf(err) != reason) {
		t.Errorf("%s: %v (reason %q) を期待したが %v / %q", name, code, reason, err, reasonOf(err))
	}
}

// 承認の署名の検証: 規則を 1 つずつ赤にできる形で確かめる。
func TestApproveSignatureRules(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	at := fmtTime(e.clock.T)
	good := sign(e.priv, p.Hash, at)

	_, err := e.approveCall(p.ChangeID, "", at)
	want(t, "署名なし", err, contract.CodeArgInvalid, "missing")
	_, err = e.approveCall(p.ChangeID, "x", at)
	want(t, "署名の形が違う", err, contract.CodeSignatureInvalid, "bad_signature")

	_, other, _ := ed25519.GenerateKey(rand.Reader)
	_, err = e.approveCall(p.ChangeID, sign(other, p.Hash, at), at)
	want(t, "別の鍵の署名", err, contract.CodeSignatureInvalid, "bad_signature")

	_, err = e.approveCall(p.ChangeID, sign(e.priv, "sha256:"+strings.Repeat("0", 64), at), at)
	want(t, "別の内容への署名", err, contract.CodeSignatureInvalid, "bad_signature")

	_, err = e.approveCall(p.ChangeID, good, "2026-11-02T00:00:01Z")
	want(t, "signed_at を変えた署名", err, contract.CodeSignatureInvalid, "bad_signature")

	old := fmtTime(e.clock.T.Add(-contract.SignedAtTolerance - 1e9))
	_, err = e.approveCall(p.ChangeID, sign(e.priv, p.Hash, old), old)
	want(t, "signed_at が古い", err, contract.CodeSignatureInvalid, "clock")
	fut := fmtTime(e.clock.T.Add(contract.SignedAtTolerance + 2e9))
	_, err = e.approveCall(p.ChangeID, sign(e.priv, p.Hash, fut), fut)
	want(t, "signed_at が先", err, contract.CodeSignatureInvalid, "clock")

	e.st.Items["approval_key"] = nil
	_, err = e.approveCall(p.ChangeID, good, at)
	want(t, "承認鍵の登録なし", err, contract.CodeSignatureInvalid, "unknown_key")
	e.st.Items["approval_key"] = json.RawMessage(`{"key_fp":"SHA256:x","key_type":"fido2","public_key":"x"}`)
	_, err = e.approveCall(p.ChangeID, good, at)
	want(t, "fido2 は検証できず通さない", err, contract.CodeSignatureInvalid, "")
}

// 内容を変えた後の署名: 保存した記録を書き換えると、計算し直した hash が記録と違い承認されない。
func TestApproveRejectsTamperedRecord(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	at := fmtTime(e.clock.T)
	sig := sign(e.priv, p.Hash, at) // 提案の時点の hash への署名
	raw, _, _ := e.a.store().b.read(changeName(p.ChangeID))
	raw = []byte(strings.Replace(string(raw), "weekly", "monthly", 1))
	_ = e.a.store().b.write(changeName(p.ChangeID), raw)
	_, err := e.approveCall(p.ChangeID, sig, at)
	want(t, "記録の書き換え", err, contract.CodeSignatureInvalid, "hash_mismatch")
}

func TestApproveFlowAndStates(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	at := fmtTime(e.clock.T)
	sig := sign(e.priv, p.Hash, at)
	r, err := e.approveCall(p.ChangeID, sig, at)
	if err != nil {
		t.Fatal(err)
	}
	var out approveOut
	_ = json.Unmarshal(r.Result, &out)
	if out.State != "approved" || out.NotBefore != at {
		t.Errorf("遅延 0 の承認: %+v", out)
	}
	if _, err := e.approveCall(p.ChangeID, sig, at); err != nil { // 同じ署名の再提出は変化なし
		t.Errorf("同じ署名の再提出: %v", err)
	}
	at2 := fmtTime(e.clock.T.Add(1e9))
	_, err = e.approveCall(p.ChangeID, sign(e.priv, p.Hash, at2), at2)
	want(t, "承認済みへの 2 つ目の署名", err, contract.CodeChangeStateInvalid, "")

	q := e.propose("key-rotate-waive", nil)
	if _, err := e.a.Cancel(ctx, call("cancel", nil, map[string][]string{"id": {q.ChangeID}})); err != nil {
		t.Fatal(err)
	}
	_, err = e.approveCall(q.ChangeID, sign(e.priv, q.Hash, at), at)
	want(t, "取り消し済みへの承認", err, contract.CodeChangeStateInvalid, "")
	_, err = e.approveCall("../../x", sig, at)
	want(t, "不正な id", err, contract.CodeChangeStateInvalid, "")
}

func TestApproveExpiredProposal(t *testing.T) {
	e := newEnv(t, true)
	p := e.propose("restore-test-increase", incr)
	e.clock.Advance(contract.ProposalExpiry)
	at := fmtTime(e.clock.T)
	_, err := e.approveCall(p.ChangeID, sign(e.priv, p.Hash, at), at)
	want(t, "期限切れの未承認", err, contract.CodeChangeStateInvalid, "")
	if l := pendingChanges(t, e.a, map[string][]string{"id": {p.ChangeID}}); len(l) != 1 || l[0].State != "expired" {
		t.Errorf("expired にならない: %+v", l)
	}
}
