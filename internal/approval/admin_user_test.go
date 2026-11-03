package approval

// D-0166: source-change の admin_user(再登録に使う管理用の利用者。定義にも節のハッシュにも入れない)と、提案時のホスト鍵の取得(Surveyor)。

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

const defForHostKey = `{"source":{"host":"a.example.invalid","targets":[{"id":"t1","files":"/d"}],"declined":[]},"restore":{"adapter":"ssh-host","dest":{"at":"ssh://o@d.example.invalid:22","key_ref":"k"}}}`

func TestSourceChangeAdminUserRules(t *testing.T) {
	e := newEnv(t, true)
	e.setDef(defForHostKey)
	// 再登録を伴う変更(ホストを替える)で admin_user が無ければ提案を作らない。
	want(t, "admin_user なし", proposeErr(e, "source-change", map[string][]string{"source": {`{"host":"b.example.invalid","targets":[{"id":"t1","files":"/d"}],"declined":[]}`}}), contract.CodeArgInvalid, "")
	want(t, "admin_user の形", proposeErr(e, "source-change", map[string][]string{"source": {`{"host":"b.example.invalid","admin_user":"a b","targets":[{"id":"t1","files":"/d"}]}`}}), contract.CodeArgInvalid, "")
	// source.declined だけの変更は要らない。
	if err := proposeErr(e, "source-change", map[string][]string{"source": {`{"host":"a.example.invalid","targets":[{"id":"t1","files":"/d"}],"declined":[{"id":"d1","mount":"/mnt/x"}]}`}}); err != nil {
		t.Errorf("declined だけの変更に admin_user を要求した: %v", err)
	}
	// admin_user があれば plan に載り、plan.after には入らない(定義の節と同じ形)。
	p := e.propose("source-change", map[string][]string{"source": {`{"host":"b.example.invalid","admin_user":"deploy","targets":[{"id":"t1","files":"/d"}]}`}})
	plan := e.planOf(p.ChangeID)
	if !strings.Contains(plan, `"admin_user":"deploy"`) || strings.Contains(plan, `"after":{"admin_user"`) || strings.Contains(plan[strings.Index(plan, `"after"`):strings.Index(plan, `"after"`)+80], "admin_user") {
		t.Errorf("plan: %s", plan)
	}
}

func TestApplySourceEnrollHostUsesAdminUser(t *testing.T) {
	e := newEnv(t, true)
	en := &recEnroller{}
	e.a.Enroller = en
	e.setDef(defForHostKey)
	p := e.propose("source-change", map[string][]string{"source": {`{"host":"b.example.invalid","admin_user":"deploy","host_key_fp":"` + fpUnit + `","targets":[{"id":"t1","files":"/d"}]}`}})
	e.approveAndWait(p, 24)
	call := contract.Call{Words: []string{"apply"}, Args: map[string][]string{"id": {p.ChangeID}, "source-admin-ref": {"adm"}}, Secrets: contract.SecretInput{"adm": json.RawMessage(`"K"`)}}
	if _, err := e.a.ApplyPlan(ctx, call, "i-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.a.ApplyEnroll(ctx, p.ChangeID, 0); err != nil {
		t.Fatal(err)
	}
	if len(en.in) != 1 || en.in[0].Host != "ssh://deploy@b.example.invalid:22" {
		t.Errorf("source enroll の --host = %+v", en.in)
	}
	if strings.Contains(string(en.in[0].Section), "admin_user") {
		t.Errorf("登録に渡す節に admin_user がある: %s", en.in[0].Section)
	}
}

func TestProposeFetchesHostKeyWithoutCredential(t *testing.T) {
	e := newEnv(t, true)
	sv := &fake.Surveyor{FP: fpUnit}
	e.a.Surveyor = sv
	e.setDef(defForHostKey)
	// 省いた host_key_fp は取得した指紋が plan と args に入る。
	p := e.propose("source-change", map[string][]string{"source": {`{"host":"b.example.invalid","admin_user":"deploy","targets":[{"id":"t1","files":"/d"}]}`}})
	if plan := e.planOf(p.ChangeID); !strings.Contains(plan, `"host_key_fp":"`+fpUnit+`"`) {
		t.Errorf("取得した指紋が plan に無い: %s", plan)
	}
	if len(sv.Fetched) != 1 || sv.Fetched[0] != "b.example.invalid:22" {
		t.Errorf("取得の呼び出し: %v", sv.Fetched)
	}
	// 書かれた値と違えば提案を作らない。
	want(t, "指紋の食い違い", proposeErr(e, "source-change", map[string][]string{"source": {`{"host":"c.example.invalid","admin_user":"deploy","host_key_fp":"SHA256:other","targets":[{"id":"t1","files":"/d"}]}`}}), contract.CodeSourceHostkeyMismatch, "")
	// restore-change の ssh-host も同じ(宛先は restore.dest.at)。
	r := e.propose("restore-change", map[string][]string{"restore": {`{"adapter":"ssh-host","dest":{"at":"ssh://o@z.example.invalid:2222","key_ref":"k"}}`}})
	if plan := e.planOf(r.ChangeID); !strings.Contains(plan, `"host_key_fp":"`+fpUnit+`"`) || sv.Fetched[len(sv.Fetched)-1] != "z.example.invalid:2222" {
		t.Errorf("restore-change: %s %v", plan, sv.Fetched)
	}
	want(t, "復元先の食い違い", proposeErr(e, "restore-change", map[string][]string{"restore": {`{"adapter":"ssh-host","dest":{"at":"ssh://o@y.example.invalid:22","key_ref":"k"},"host_key_fp":"SHA256:other"}`}}), contract.CodeRestoreHostkeyMismatch, "")
}

// 届かないときは提案を作らず(STORAGE_UNREACHABLE の target: source / restore_host)、dry-run は plan.errors[] に載せる。
func TestProposeHostKeyUnreachable(t *testing.T) {
	e := newEnv(t, true)
	e.a.Surveyor = &fake.Surveyor{Err: &contract.SSHError{Reason: contract.SSHReasonConnect}}
	e.setDef(defForHostKey)
	src := map[string][]string{"source": {`{"host":"b.example.invalid","admin_user":"deploy","targets":[{"id":"t1","files":"/d"}]}`}}
	err := proposeErr(e, "source-change", src)
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeStorageUnreachable || !strings.Contains(string(ce.Detail), `"target":"source"`) {
		t.Fatalf("source: %v", err)
	}
	rst := map[string][]string{"restore": {`{"adapter":"ssh-host","dest":{"at":"ssh://o@z.example.invalid:22","key_ref":"k"}}`}}
	if err := proposeErr(e, "restore-change", rst); !errors.As(err, &ce) || ce.Code != contract.CodeStorageUnreachable || !strings.Contains(string(ce.Detail), `"target":"restore_host"`) {
		t.Fatalf("restore: %v", err)
	}
	src["dry-run"] = []string{}
	r, err := e.a.Propose(ctx, contract.Call{Words: []string{"propose"}, Positional: []string{"source-change"}, Args: src, DryRun: true})
	if err != nil || !strings.Contains(string(r.Result), `"STORAGE_UNREACHABLE"`) {
		t.Errorf("dry-run の plan.errors: %v %s", err, r.Result)
	}
	if evs, _, _, _ := e.a.store().events(); len(evs) != 0 {
		t.Errorf("提案ができた: %d 件の事象", len(evs))
	}
}
