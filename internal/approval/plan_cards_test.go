package approval

// まとめレビュー H1: 承認カードの材料(spec 3.1.2.9 の表の plan の列)を核が作る。実物の propose の記録を app/test/u3a/fixtures/propose-records.json に
// 書き出し(UPDATE_PROPOSE_FIXTURE=1)、アプリの buildCard の試験(app/test/u3a/card-propose.test.ts)が読んで、承認が有効になり宛先と指紋が行に出ることを確かめる。
// ここでは plan が契約の schema に合うこと、カードに出る欄が揃うこと、書き出した記録が固定ファイルと同じであることを確かめる。

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

const proposeFixture = "../../app/test/u3a/fixtures/propose-records.json"

const (
	fixBase   = `"version":1,"storage":[{"endpoint":"https://s3.example.invalid","bucket":"bkt","key_ref":"store-main"}],"notify":{"to":["ops@example.invalid"]}`
	fixDef    = `{` + fixBase + `,"approval_delay_hours":24,"restore":{"adapter":"ssh-host","dest":{"at":"ssh://ops@dest.example.invalid:22","key_ref":"dest-key"}},"source":{"host":"app01.example.invalid","targets":[{"id":"t1","files":"/srv/app/data"}]}}`
	fixSealed = `{` + fixBase + `,"approval_delay_hours":8,"restore":{"adapter":"ssh-host","dest":{"at":"ssh://ops@dest.example.invalid:22","key_ref":"dest-key"}},"source":{"host":"app01.example.invalid","targets":[{"id":"t1","files":"/srv/app/data"}]}}`
	fixKeyFP  = "SHA256:K1e2Y3f4P5k6F7p8A9b0C1d2E3f4G5h6I7j8K9l0M1n"
	fixSrcFP  = "SHA256:" + "A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8S9t0U1v"
)

// fixedKey は固定ファイルが毎回同じになるよう、承認鍵の記録を固定の値にする。
func fixedKey(e *env) {
	e.st.Items["approval_key"] = json.RawMessage(`{"key_fp":"` + fixKeyFP + `","key_type":"software","public_key":"ssh-ed25519 AAAA"}`)
}

func (e *env) proposeShape(op string, args map[string][]string) json.RawMessage {
	e.t.Helper()
	r, err := e.a.Propose(ctx, contract.Call{Words: []string{"propose"}, Positional: []string{op}, Args: args})
	harness.Shape(e.t, []string{"propose"}, r, err)
	var p proposeRes
	_ = json.Unmarshal(r.Result, &p)
	rec, _, err := e.a.store().readChange(p.ChangeID)
	if err != nil {
		e.t.Fatal(err)
	}
	b, err := marshalCanonical(rec)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func planOfRec(t *testing.T, rec json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var r struct {
		Plan map[string]json.RawMessage `json:"plan"`
	}
	if err := json.Unmarshal(rec, &r); err != nil {
		t.Fatal(err)
	}
	return r.Plan
}

func TestProposePlansCarryCardMaterial(t *testing.T) {
	var recs []json.RawMessage

	// seal(seal の前: 候補を確定する)。ホスト鍵の固定値と承認鍵の指紋が plan に載る。
	es := newEnv(t, true)
	fixedKey(es)
	es.st.Items["sealed"] = json.RawMessage(`false`)
	es.st.Items["host_keys"] = json.RawMessage(`[{"role":"source","host":"app01.example.invalid","host_key_fp":"` + fixSrcFP + `","session_id":null}]`)
	es.a.State = &candState{StateFiles: es.st, cand: []byte(fixSealed), candHash: "sha256:" + strings.Repeat("1", 64), defHash: "sha256:" + strings.Repeat("1", 64)}
	es.withDef(fixSealed)
	rec := es.proposeShape("seal", map[string][]string{"file": {"-"}})
	p := planOfRec(t, rec)
	for _, k := range []string{"definition", "host_keys", "approval_key_fp", "weaker_items", "definition_from"} {
		if _, ok := p[k]; !ok {
			t.Errorf("seal の plan に %s が無い: %s", k, rec)
		}
	}
	if _, bad := p["def_hash_after"]; bad {
		t.Errorf("seal の plan に def_hash_after がある(カードの欄に無く承認を無効にする)")
	}
	if !strings.Contains(string(p["host_keys"]), fixSrcFP) || !strings.Contains(string(p["weaker_items"]), "/approval_delay_hours") {
		t.Errorf("seal: host_keys=%s weaker_items=%s", p["host_keys"], p["weaker_items"])
	}
	recs = append(recs, rec)

	recs = append(recs, proposeSectionKinds(t)...)

	got, _ := json.MarshalIndent(recs, "", " ")
	got = append(got, '\n')
	if os.Getenv("UPDATE_PROPOSE_FIXTURE") != "" {
		if err := os.WriteFile(proposeFixture, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(proposeFixture)
	if err != nil || string(want) != string(got) {
		t.Errorf("%s が実物の propose の記録と違う(UPDATE_PROPOSE_FIXTURE=1 で書き直す): %v", proposeFixture, err)
	}
}

// 承認済みの定義の後の種別(source-change・restore-change・delay-change・def-change)。
func proposeSectionKinds(t *testing.T) []json.RawMessage {
	var recs []json.RawMessage
	var rec json.RawMessage
	var p map[string]json.RawMessage
	e := newEnv(t, true)
	fixedKey(e)
	e.setDef(fixDef)
	e.a.Planner = nil
	srcNew := `{"host":"attacker.example.invalid","admin_user":"deploy","host_key_fp":"` + fixSrcFP + `","targets":[{"id":"t1","files":"/srv/app/data"}]}`
	rec = e.proposeShape("source-change", map[string][]string{"source": {srcNew}})
	p = planOfRec(t, rec)
	if !strings.Contains(string(p["after"]), "attacker.example.invalid") || !strings.Contains(string(p["before"]), "app01.example.invalid") ||
		string(p["host_key_fp"]) != `"`+fixSrcFP+`"` || string(p["needs_secret"]) != "true" {
		t.Errorf("source-change の plan: %s", rec)
	}
	recs = append(recs, rec)

	rec = e.proposeShape("restore-change", map[string][]string{"restore": {`{"adapter":"ssh-host","dest":{"at":"ssh://ops@other.example.invalid:22","key_ref":"dest-key"},"host_key_fp":"` + fixSrcFP + `"}`}})
	p = planOfRec(t, rec)
	if !strings.Contains(string(p["after"]), "other.example.invalid") || string(p["host_key_fp"]) != `"`+fixSrcFP+`"` {
		t.Errorf("restore-change の plan: %s", rec)
	}
	recs = append(recs, rec)

	rec = e.proposeShape("delay-change", map[string][]string{"hours": {"48"}})
	p = planOfRec(t, rec)
	if string(p["before"]) != "24" || string(p["after"]) != "48" {
		t.Errorf("delay-change の plan: %s", rec)
	}
	recs = append(recs, rec)

	e.withDef(strings.Replace(fixDef, `"approval_delay_hours":24`, `"approval_delay_hours":24,"cost_ceiling_yen":20000`, 1))
	e.a.State = &candState{StateFiles: e.st, defHash: "sha256:" + strings.Repeat("3", 64)}
	rec = e.proposeShape("def-change", map[string][]string{"file": {"-"}})
	p = planOfRec(t, rec)
	if !strings.Contains(string(p["diff"]), "/cost_ceiling_yen") || string(p["cost_ceiling_yen"]) != "20000" {
		t.Errorf("def-change の plan: %s", rec)
	}
	recs = append(recs, rec)

	return recs
}
