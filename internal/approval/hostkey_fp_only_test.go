package approval

// まとめレビュー F4: host_key_fp だけを変える提案は、取得も登録も通らずに固定値を差し替えられてはならない。
// 今の固定値(host_keys[])と違う指紋は needs_secret を真にして、資格情報なしの取得(Surveyor)と登録へ回す。同じ指紋なら登録を要さない。

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

const (
	fpPinned   = "SHA256:Pinned0123456789abcdefghijklmnopqrstuvwxyzAB"
	fpAttacker = "SHA256:AttackerChosenFingerprint"
)

func fpEnv(t *testing.T) (*env, *fake.Surveyor) {
	e := newEnv(t, true)
	sv := &fake.Surveyor{FP: fpUnit}
	e.a.Surveyor = sv
	e.setDef(defForHostKey)
	e.st.Items["host_keys"] = json.RawMessage(`[{"role":"source","host":"a.example.invalid","host_key_fp":"` + fpPinned + `"},{"role":"restore_dest","host":"d.example.invalid","host_key_fp":"` + fpPinned + `"}]`)
	return e, sv
}

const srcFP = `{"host":"a.example.invalid","admin_user":"deploy","host_key_fp":"%s","targets":[{"id":"t1","files":"/d"}],"declined":[]}`
const rstFP = `{"adapter":"ssh-host","dest":{"at":"ssh://o@d.example.invalid:22","key_ref":"k"},"host_key_fp":"%s"}`

func TestFPOnlyChangeGoesThroughFetch(t *testing.T) {
	for _, c := range []struct{ kind, key, tmpl string }{{"source-change", "source", srcFP}, {"restore-change", "restore", rstFP}} {
		// 今の固定値と違う指紋で、取得した指紋と違う: 取得が走り、提案を作らない。
		e, sv := fpEnv(t)
		err := proposeErr(e, c.kind, map[string][]string{c.key: {fmt.Sprintf(c.tmpl, fpAttacker)}})
		if len(sv.Fetched) != 1 {
			t.Errorf("%s: 取得 %v", c.kind, sv.Fetched)
		}
		if codeOf(err) != contract.CodeSourceHostkeyMismatch && codeOf(err) != contract.CodeRestoreHostkeyMismatch {
			t.Errorf("%s: 取得した指紋と違う固定値を通した: %v", c.kind, err)
		}
		// 取得した指紋と同じ(正当な鍵の更新): needs_secret が真で、登録の段を含む。
		e, sv = fpEnv(t)
		p := e.propose(c.kind, map[string][]string{c.key: {fmt.Sprintf(c.tmpl, fpUnit)}})
		if plan := e.planOf(p.ChangeID); !strings.Contains(plan, `"needs_secret":true`) || len(sv.Fetched) != 1 {
			t.Errorf("%s: plan=%s fetched=%v", c.kind, plan, sv.Fetched)
		}
		rec, _, _ := e.a.store().readChange(p.ChangeID)
		hasEnroll := false
		for _, s := range fullSteps(rec) {
			hasEnroll = hasEnroll || s.Kind == stepEnroll
		}
		if !hasEnroll {
			t.Errorf("%s: 登録の段が無い", c.kind)
		}
		// 今の固定値と同じ指紋: 登録を要さず、取得も走らない。
		e, sv = fpEnv(t)
		p = e.propose(c.kind, map[string][]string{c.key: {fmt.Sprintf(c.tmpl, fpPinned)}})
		if plan := e.planOf(p.ChangeID); !strings.Contains(plan, `"needs_secret":false`) || len(sv.Fetched) != 0 {
			t.Errorf("%s(同じ指紋): plan=%s fetched=%v", c.kind, plan, sv.Fetched)
		}
	}
}

// F7: 取得が時限切れなら plan.errors[] の reason は timeout(届かないのは connect)。
func TestFetchTimeoutReasonInPlan(t *testing.T) {
	for _, reason := range []string{contract.SSHReasonTimeout, contract.SSHReasonConnect} {
		e, sv := fpEnv(t)
		sv.Err = &contract.SSHError{Reason: reason}
		args := map[string][]string{"source": {fmt.Sprintf(srcFP, fpUnit)}}
		r, err := e.a.Propose(ctx, contract.Call{Words: []string{"propose"}, Positional: []string{"source-change"}, Args: args, DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(r.Result), `"reason":"`+reason+`"`) {
			t.Errorf("%s: %s", reason, r.Result)
		}
	}
}
