package approval

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

// 4.6 の署名の対象 "sitate-approval/v1\n" + canonical_hash + "\n" + signed_at を固定した試験ベクトル(アプリ側と文字列を合わせる)。
func TestSignatureVector(t *testing.T) {
	e := newEnv(t, true)
	k, _ := json.Marshal(map[string]string{"key_fp": "SHA256:lbmsoA0yIEcEiVDRnMWuzm+nV+3ZEEpVIURqFoeSspg", "key_type": "software",
		"public_key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAOhB7/zzhC+HXDdGOdLwJln5NYwm6UNXx3chmQSVTG4"})
	e.st.Items["approval_key"] = k
	hash := "sha256:" + strings.Repeat("a", 64)
	sig := "k7yR8HG4L9EAUg3kT6RfnxR1uM4fTvOJQpwq3k/xN46BSPRJ3Aef/Oq67BwTULl9T/WaJokP7/oWQqv/YvPvCA=="
	if _, err := e.a.verifySignature(ctx, hash, "2026-11-02T00:00:00Z", sig); err != nil {
		t.Fatalf("固定のベクトル: %v", err)
	}
	if _, err := e.a.verifySignature(ctx, hash, "2026-11-02T00:00:01Z", sig); err == nil {
		t.Error("signed_at を変えても通る")
	}
}

func newProofEnv(t *testing.T) (*env, string) {
	e := newEnv(t, true)
	salt := []byte("0123456789abcdef")
	h := wordHash(salt, "ocean rapid tiger")
	e.a.TestNotifier = &fakeProofs{out: map[string]string{"b@x": base64.StdEncoding.EncodeToString(salt) + "|" + h}}
	e.setDef(`{"notify":{"to":["a@x"]}}`)
	return e, h
}

func approveWithProof(e *env, p proposeRes, proof string) error {
	at := fmtTime(e.clock.T)
	args := map[string][]string{"id": {p.ChangeID}, "signature": {sign(e.priv, p.Hash, at)}, "signed-at": {at}}
	if proof != "" {
		args["notify-proof-stdin"] = []string{proof}
	}
	_, err := e.a.Approve(ctx, contract.Call{Words: []string{"approve"}, Args: args})
	return err
}

// notify-add の確認語: proof_required は核が計算し、一致・不一致・欠け・対象外のキーを区別する。承認の受理で change_proofs[] を消す。
func TestNotifyProofs(t *testing.T) {
	e, _ := newProofEnv(t)
	p := e.propose("notify-add", map[string][]string{"notify": {`{"to":["a@x","b@x"]}`}})
	var plan struct {
		Required []string `json:"proof_required"`
	}
	_ = json.Unmarshal([]byte(e.planOf(p.ChangeID)), &plan)
	if len(plan.Required) != 1 || plan.Required[0] != "b@x" {
		t.Fatalf("proof_required: %s", e.planOf(p.ChangeID))
	}
	for name, in := range map[string]string{"なし": "", "欠け": `{}`, "違う語": `{"b@x":"ocean rapid lion"}`, "対象外のキー": `{"b@x":"ocean rapid tiger","a@x":"x"}`, "壊れた入力": `[`} {
		want(t, name, approveWithProof(e, p, in), contract.CodeNotifyProofMismatch, "")
	}
	if err := approveWithProof(e, p, `{"b@x":"  OCEAN　rapid   Tiger "}`); err != nil {
		t.Fatalf("正規化した一致: %v", err)
	}
	raw, _ := e.st.Get(ctx, "notify")
	if strings.Contains(string(raw), p.ChangeID) {
		t.Errorf("change_proofs が残る: %s", raw)
	}
	// 試験通知の依存が無ければ提案を作らない。
	e2 := newEnv(t, true)
	want(t, "TestNotifier なし", proposeErr(e2, "notify-add", map[string][]string{"notify": {`{"to":["z@x"]}`}}), contract.CodeInternal, "")
}

func TestSealProofRules(t *testing.T) {
	e := newEnv(t, true)
	cs := &candState{StateFiles: e.st, cand: []byte(`{"notify":{"to":[]}}`), candHash: "sha256:" + strings.Repeat("1", 64), defHash: "sha256:" + strings.Repeat("1", 64)}
	e.a.State = cs
	e.st.Items["sealed"] = json.RawMessage(`false`)
	e.withDef(`{"notify":{"to":[]}}`)
	p := e.propose("seal", map[string][]string{"file": {"-"}})
	want(t, "照合の対象が空の seal", approveWithProof(e, p, `{}`), contract.CodeNotifyProofMismatch, "")

	e2 := newEnv(t, true)
	salt := []byte("0123456789abcdef")
	e2.st.Items["notify"] = mustJSON(map[string]any{"test_proofs": []proofRow{{To: "a@x", Salt: base64.StdEncoding.EncodeToString(salt), Hash: wordHash(salt, "one two three")}}})
	e2.st.Items["sealed"] = json.RawMessage(`false`)
	cs2 := &candState{StateFiles: e2.st, cand: []byte(`{"notify":{"to":["a@x","c@x"]}}`), candHash: "sha256:" + strings.Repeat("2", 64), defHash: "sha256:" + strings.Repeat("2", 64)}
	e2.a.State = cs2
	e2.withDef(`{"notify":{"to":["a@x","c@x"]}}`)
	p2 := e2.propose("seal", map[string][]string{"file": {"-"}})
	want(t, "試験通知を送っていない宛先", approveWithProof(e2, p2, `{"a@x":"one two three","c@x":"x"}`), contract.CodeNotifyProofMismatch, "")
	want(t, "違う語", approveWithProof(e2, p2, `{"a@x":"one two four"}`), contract.CodeNotifyProofMismatch, "")
}

func TestSealRules(t *testing.T) {
	e := newEnv(t, true)
	cs := &candState{StateFiles: e.st, cand: []byte(`{}`), candHash: "sha256:" + strings.Repeat("1", 64), defHash: "sha256:" + strings.Repeat("2", 64)}
	e.a.State = cs
	e.withDef(`{}`)
	want(t, "seal 済み", proposeErr(e, "seal", map[string][]string{"file": {"-"}}), contract.CodeAlreadySealed, "")
	e.st.Items["sealed"] = json.RawMessage(`false`)
	err := proposeErr(e, "seal", map[string][]string{"file": {"-"}})
	want(t, "候補と違う def_hash", err, contract.CodeDefInvalid, "")
	var ce *contract.Error
	if asContractErr(err, &ce) && (len(ce.Next) != 1 || ce.Next[0].Argv[len(ce.Next[0].Argv)-1] != "-") {
		t.Errorf("next: %+v", ce.Next)
	}
	cs.defHash = cs.candHash
	if err := proposeErr(e, "seal", map[string][]string{"file": {"-"}}); err != nil {
		t.Errorf("候補と同じ: %v", err)
	}
}

// --file - と --file <path> は同じ内容なら同じ変更になり、Planner は定義を見て、Planner の誤りで提案を作らない。
func TestDefinitionPlanner(t *testing.T) {
	e := newEnv(t, true)
	e.a.State = &candState{StateFiles: e.st, defHash: "sha256:" + strings.Repeat("3", 64)}
	e.setDef(`{"schedule":"02:30"}`)
	e.withDef(`{"schedule":"03:00"}`)
	var seen string
	e.a.Planner = func(_ ctxT, op string, args map[string]json.RawMessage) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
		seen = string(args["definition"])
		return map[string]json.RawMessage{"diff": json.RawMessage(`[]`)}, nil, nil
	}
	p1 := e.propose("def-change", map[string][]string{"file": {"-"}})
	p2 := e.propose("def-change", map[string][]string{"file": {"/etc/sitate/x.yaml"}})
	if !p2.Reused || p2.ChangeID != p1.ChangeID || !strings.Contains(seen, "03:00") || !strings.Contains(e.planOf(p1.ChangeID), `"diff"`) {
		t.Errorf("同値・Planner: %+v seen=%s plan=%s", p2, seen, e.planOf(p1.ChangeID))
	}
	e.a.Planner = func(ctxT, string, map[string]json.RawMessage) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
		return nil, nil, &contract.Error{Code: contract.CodeInternal}
	}
	e.withDef(`{"schedule":"05:00"}`)
	if err := proposeErr(e, "def-change", map[string][]string{"file": {"-"}}); err == nil {
		t.Error("Planner の誤りで提案が作られた")
	}
	if l := pendingChanges(t, e.a, nil); len(l) != 1 {
		t.Errorf("提案の数: %d", len(l))
	}
}

func TestHostKeyPinning(t *testing.T) {
	e := newEnv(t, true)
	enr := &recEnroller{}
	e.a.Enroller = enr
	e.a.Planner = func(_ ctxT, op string, _ map[string]json.RawMessage) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
		return map[string]json.RawMessage{"host_key_fp": json.RawMessage(`"SHA256:CARD"`)}, nil, nil
	}
	p := e.propose("source-change", map[string][]string{"source": {`{"host":"ssh://u@h:22"}`}})
	e.approveAndWait(p, 24)
	call := contract.Call{Words: []string{"apply"}, Args: map[string][]string{"id": {p.ChangeID}, "source-admin-ref": {"r"}}, Secrets: contract.SecretInput{"r": json.RawMessage(`"k"`)}}
	plan, err := e.a.ApplyPlan(ctx, call, "i-1")
	if err != nil || len(plan.Changes) != 1 {
		t.Fatalf("%+v %v", plan, err)
	}
	if _, err := e.a.ApplyEnroll(ctx, p.ChangeID, 0); err != nil || len(enr.in) != 1 || enr.in[0].HostKeyFP != "SHA256:CARD" || enr.in[0].AdminRef != "r" || enr.in[0].Admin == nil || string(enr.in[0].Admin.Reveal()) != `"k"` {
		t.Fatalf("カードの指紋が登録に届かない: %+v %v", enr.in, err)
	}
	// 節に別の指紋があれば提案を作らない。
	want(t, "指紋の食い違い", proposeErr(e, "source-change", map[string][]string{"source": {`{"host":"x","host_key_fp":"SHA256:OTHER"}`}}), contract.CodeSourceHostkeyMismatch, "")
	// 指紋が無いまま登録へ進まない。
	e2 := newEnv(t, true)
	e2.a.Enroller = &recEnroller{}
	q := e2.propose("source-change", map[string][]string{"source": {`{"host":"h"}`}})
	e2.approveAndWait(q, 24)
	if _, err := e2.apply(map[string][]string{"id": {q.ChangeID}, "source-admin-ref": {"r"}}); err != nil {
		t.Fatal(err)
	}
	_, err = e2.a.ApplyEnroll(ctx, q.ChangeID, 0)
	want(t, "空の指紋", err, contract.CodeSourceHostkeyMismatch, "")
}

var _ = fake.StartTime
var _ = time.Second
var _ = sha256.Size
