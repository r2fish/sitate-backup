package approval

// まとめレビュー F3: 提案の節を取得・登録・カードの前に厳密に検査する。Go の JSON は大文字小文字を畳んでキーを読む(`ſ` は `s` と同じ)ので、
// 未知のキーと畳み込みの重複は DEF_INVALID で止め、Surveyor の取得 0 回・提案 0 件にする。実物の検査(state.StateFiles.ValidateDef)を差し込む。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/state"
)

// strictState は ValidateDef だけ実物にした StateFiles。
type strictState struct {
	*fake.StateFiles
	real *state.StateFiles
}

func (s *strictState) ValidateDef(ctx context.Context, d []byte, skip bool) ([]byte, string, json.RawMessage, error) {
	return s.real.ValidateDef(ctx, d, skip)
}

func strictEnv(t *testing.T) (*env, *fake.Surveyor) {
	e := newEnv(t, true)
	e.a.State = &strictState{StateFiles: e.st, real: &state.StateFiles{Dir: t.TempDir()}}
	e.a.TestNotifier = &fakeProofs{out: map[string]string{}}
	sv := &fake.Surveyor{FP: fpUnit}
	e.a.Surveyor = sv
	e.setDef(`{"version":1,"source":{"host":"a.example.invalid","targets":[{"id":"t1","files":"/d"}]},"storage":[{"endpoint":"https://s3.example.invalid","bucket":"b","key_ref":"store-main"}],` +
		`"restore":{"adapter":"ssh-host","dest":{"at":"ssh://o@d.example.invalid:22","key_ref":"k"}},"notify":{"to":["a@x"]}}`)
	return e, sv
}

func proposalCount(t *testing.T, e *env) int {
	evs, _, _, err := e.a.store().events()
	if err != nil {
		t.Fatal(err)
	}
	return len(evs)
}

func TestProposeRejectsFoldedAndUnknownKeys(t *testing.T) {
	cases := []struct{ name, kind, key, sec string }{
		{"source: hoſt", "source-change", "source", `{"host":"good.example.invalid","hoſt":"evil.example.invalid","admin_user":"deploy","targets":[{"id":"t1","files":"/d"}]}`},
		{"source: HOST", "source-change", "source", `{"HOST":"evil.example.invalid","host":"good.example.invalid","admin_user":"deploy","targets":[{"id":"t1","files":"/d"}]}`},
		{"source: 未知のキー", "source-change", "source", `{"host":"good.example.invalid","admin_user":"deploy","port":2222,"targets":[{"id":"t1","files":"/d"}]}`},
		{"source: Admin_User", "source-change", "source", `{"host":"good.example.invalid","admin_user":"deploy","Admin_User":"root","targets":[{"id":"t1","files":"/d"}]}`},
		{"restore: deſt", "restore-change", "restore", `{"adapter":"ssh-host","dest":{"at":"ssh://o@good.example.invalid:22","key_ref":"k"},"deſt":{"at":"ssh://o@evil.example.invalid:22"}}`},
		{"restore: 入れ子の At", "restore-change", "restore", `{"adapter":"ssh-host","dest":{"at":"ssh://o@good.example.invalid:22","key_ref":"k","At":"ssh://o@evil.example.invalid:22"}}`},
		{"restore: admin_user", "restore-change", "restore", `{"adapter":"ssh-host","dest":{"at":"ssh://o@good.example.invalid:22","key_ref":"k"},"admin_user":"root"}`},
		{"notify: TO", "notify-add", "notify", `{"to":["a@x","b@x"],"TO":[]}`},
	}
	for _, c := range cases {
		e, sv := strictEnv(t)
		before := proposalCount(t, e)
		err := proposeErr(e, c.kind, map[string][]string{c.key: {c.sec}})
		want(t, c.name, err, contract.CodeDefInvalid, "")
		if len(sv.Fetched) != 0 {
			t.Errorf("%s: 取得が走った %v", c.name, sv.Fetched)
		}
		if n := proposalCount(t, e); n != before {
			t.Errorf("%s: 提案が作られた(事象 %d → %d)", c.name, before, n)
		}
	}
}

// 正しい節は通り、args の節は検査を通した正規形(キーの順を揃えた JSON)になる。
func TestProposeKeepsCanonicalSection(t *testing.T) {
	e, sv := strictEnv(t)
	p := e.propose("source-change", map[string][]string{"source": {`{"targets":[{"files":"/d","id":"t1"}],"admin_user":"deploy","host":"b.example.invalid"}`}})
	rec, _, err := e.a.store().readChange(p.ChangeID)
	if err != nil {
		t.Fatal(err)
	}
	var args map[string]json.RawMessage
	_ = json.Unmarshal(rec.Args, &args)
	if !strings.HasPrefix(string(args["source"]), `{"admin_user":"deploy","host":"b.example.invalid","host_key_fp":`) {
		t.Errorf("args.source が正規形でない: %s", args["source"])
	}
	if len(sv.Fetched) != 1 {
		t.Errorf("取得: %v", sv.Fetched)
	}
}
