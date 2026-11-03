package approval

// まとめレビュー M2・M3: 登録の秘密は JSON の字面ではなく鍵の文字列で渡す。ssh-host の restore-change は、カードで照らした指紋が無いまま登録しない。

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const fpUnit = "SHA256:Zz9Yy8Xx7Ww6Vv5Uu4Tt3Ss2Rr1Qq0Pp9Oo8Nn7Mm6"

func TestEnrollGetsKeyTextNotJSONLiteral(t *testing.T) {
	for name, tc := range map[string]struct{ raw, want string }{
		"文字列":                    {`"-----BEGIN KEY-----\nabc\n-----END KEY-----\n"`, "-----BEGIN KEY-----\nabc\n-----END KEY-----\n"},
		"private_key を持つ object": {`{"private_key":"PEMTEXT"}`, "PEMTEXT"},
	} {
		e := newEnv(t, true)
		en := &recEnroller{}
		e.a.Enroller = en
		e.setDef(`{"source":{"host":"a.example.invalid"}}`)
		p := e.propose("source-change", map[string][]string{"source": {`{"host":"b.example.invalid","admin_user":"deploy","host_key_fp":"` + fpUnit + `"}`}})
		e.approveAndWait(p, 24)
		call := map[string][]string{"id": {p.ChangeID}, "source-admin-ref": {"adm"}}
		if _, err := e.a.ApplyPlan(ctx, contract.Call{Words: []string{"apply"}, Args: call, Secrets: contract.SecretInput{"adm": json.RawMessage(tc.raw)}}, "i-1"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// ApplyEnroll は cli が呼ぶ。同じ入口を呼んで登録の入力を作らせる。
		if _, err := e.a.ApplyEnroll(ctx, p.ChangeID, 0); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(en.in) != 1 || en.in[0].Admin == nil || string(en.in[0].Admin.Reveal()) != tc.want {
			t.Errorf("%s: 登録に渡った秘密の数 = %d(鍵の文字列 %q のはず)", name, len(en.in), tc.want)
		}
	}
}

func TestRestoreChangeSshHostWithoutFingerprintRefused(t *testing.T) {
	e := newEnv(t, true)
	en := &recEnroller{}
	e.a.Enroller = en
	e.setDef(`{"restore":{"adapter":"ssh-host","dest":{"at":"ssh://o@a.example.invalid:22","key_ref":"k"}}}`)
	p := e.propose("restore-change", map[string][]string{"restore": {`{"adapter":"ssh-host","dest":{"at":"ssh://o@b.example.invalid:22","key_ref":"k"}}`}})
	e.approveAndWait(p, 24)
	if _, err := e.a.ApplyPlan(ctx, contract.Call{Words: []string{"apply"}, Args: map[string][]string{"id": {p.ChangeID}}, Secrets: contract.SecretInput{"k": json.RawMessage(`"K"`)}}, "i-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.a.ApplyEnroll(ctx, p.ChangeID, 0); err == nil {
		t.Error("指紋の無い ssh-host の restore-change を登録に進めた")
	}
	if len(en.in) != 0 {
		t.Errorf("初期資格情報を渡した: %v", en.in)
	}
}

// M1: storage-init の提案は、承認済みの定義の storage[0] と違う保管先なら作らない(ALREADY_INITIALIZED)。同じなら plan に保管先と参照名が載る。
func TestStorageInitProposalBoundToApprovedStorage(t *testing.T) {
	e := newEnv(t, true)
	e.setDef(`{"storage":[{"endpoint":"https://s3.example.invalid","bucket":"bkt","prefix":"sitate/","key_ref":"store-main"}]}`)
	p := e.propose("storage-init", map[string][]string{"endpoint": {"https://s3.example.invalid"}, "bucket": {"bkt"}, "prefix": {"sitate/"}, "reissue": {}})
	plan := e.planOf(p.ChangeID)
	if !strings.Contains(plan, `"key_ref":"store-main"`) || !strings.Contains(plan, `"reissue":true`) {
		t.Errorf("plan: %s", plan)
	}
	want(t, "別のバケット", proposeErr(e, "storage-init", map[string][]string{"bucket": {"attacker"}}), contract.CodeAlreadyInitialized, "")
}

// toRecorder は EmitTo(宛先の追加)を覚える Recorder。
type toRecorder struct {
	*acceptRec
	extra map[string][]string
}

func (r *toRecorder) EmitTo(c ctxT, typ string, facts []byte, extra []string) (contract.Event, []contract.DeliveryResult, error) {
	r.extra[typ] = extra
	return r.acceptRec.Emit(c, typ, facts)
}

// M5: 宛先を外す notify-change の change.applied は、変更前の宛先(plan.before.to)にも送る。
func TestNotifyChangeAppliedAlsoGoesToPreviousRecipients(t *testing.T) {
	e := newEnv(t, true)
	tr := &toRecorder{acceptRec: e.rec, extra: map[string][]string{}}
	e.a.Recorder = tr
	e.a.TestNotifier = &fakeProofs{}
	e.setDef(`{"notify":{"to":["owner@x","ops@x"]}}`)
	p := e.propose("notify-change", map[string][]string{"notify": {`{"to":["ops@x"]}`}})
	e.approveAndWait(p, 24)
	call := contract.Call{Words: []string{"apply"}, Args: map[string][]string{"id": {p.ChangeID}}}
	if _, err := e.a.ApplyPlan(ctx, call, "i-1"); err != nil {
		t.Fatal(err)
	}
	if err := e.a.ApplyRecord(ctx, p.ChangeID, 0, contract.StepResult{Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	got := tr.extra["change.applied"]
	if len(got) != 2 || got[0] != "owner@x" || got[1] != "ops@x" {
		t.Errorf("変更前の宛先 = %v", got)
	}
}
