package approval

// まとめレビュー U-d: 登録の入力に写した管理用資格情報(in.Admin)は、登録が終わったら消す(mlock の下でプロセスの終了まで残さない)。

import (
	"encoding/json"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func TestApplyEnrollWipesAdminSecret(t *testing.T) {
	e := newEnv(t, true)
	en := &recEnroller{}
	e.a.Enroller = en
	e.setDef(defForHostKey)
	p := e.propose("source-change", map[string][]string{"source": {`{"host":"b.example.invalid","admin_user":"deploy","host_key_fp":"` + fpUnit + `","targets":[{"id":"t1","files":"/d"}]}`}})
	e.approveAndWait(p, 24)
	call := contract.Call{Words: []string{"apply"}, Args: map[string][]string{"id": {p.ChangeID}, "source-admin-ref": {"adm"}}, Secrets: contract.SecretInput{"adm": json.RawMessage(`"KEY-MATERIAL"`)}}
	if _, err := e.a.ApplyPlan(ctx, call, "i-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.a.ApplyEnroll(ctx, p.ChangeID, 0); err != nil {
		t.Fatal(err)
	}
	if len(en.in) != 1 || en.in[0].Admin == nil {
		t.Fatalf("登録の入力に Admin が無い: %+v", en.in)
	}
	if n := len(en.in[0].Admin.Reveal()); n != 0 {
		t.Errorf("登録の後も管理用資格情報が %d バイト残っている", n)
	}
}
