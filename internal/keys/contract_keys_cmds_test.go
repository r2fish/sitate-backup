package keys

// phrase verify・kit confirm・doctor の契約試験(作業指示書 5.7 の完了条件): 結果と状態の中身を判定する。

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// --- phrase verify ---

func TestContractKeysPhraseVerifyRejects(t *testing.T) {
	// 開けないフレーズ(または入力が無い呼び出し)は成功の形を返さず、保管先へ何も書かず、確認済みにも記録しない(確定は開けた後だけ。4.3)。
	for name, secrets := range map[string]contract.SecretInput{
		"フレーズ無し":   nil,
		"開けないフレーズ": {"phrase": json.RawMessage(`"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"`)},
	} {
		e := newEnv()
		e.ks.Open = false
		r, err := e.k.PhraseVerify(ctx, contract.Call{Words: []string{"phrase", "verify"}, Secrets: secrets, SecretOut: true})
		if err == nil {
			t.Errorf("%s: 成功の形を返した: %s", name, r.Result)
			continue
		}
		var ce *contract.Error
		harness.Implemented(t, err)
		if !errors.As(err, &ce) || (ce.Code != contract.CodePhraseInvalid && ce.Code != contract.CodeArgInvalid && ce.Code != contract.CodeNotInitialized) {
			t.Errorf("%s: 誤り = %v (PHRASE_INVALID / ARG_INVALID / NOT_INITIALIZED のどれか)", name, err)
		}
		if vs, _ := e.k.Store.ListVersions(ctx, ""); len(vs) != 0 {
			t.Errorf("%s: 確定の前に保管先へ書いた: %d 件", name, len(vs))
		}
		if v := e.st.Items["phrase_confirmed"]; string(v) == "true" || e.st.Items["last_phrase_verify"] != nil {
			t.Errorf("%s: 確認済みの記録を残した: %v", name, e.st.Items)
		}
		if r.Phrase != nil {
			t.Errorf("%s: 応答にフレーズがある", name)
		}
	}
}

// --- kit confirm ---

var kitT0 = time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)

func kitEnv() (*kenv, *fake.Clock) {
	e := newEnv()
	clk := &fake.Clock{T: kitT0}
	e.k.Clock = clk
	e.st.Items = map[string]json.RawMessage{"kit": json.RawMessage(`{"version":"kv-1","confirmed_version":null,"location_checked_at":null,"confirmed_core_version":null}`)}
	return e, clk
}

func kitCall(copies string, extra ...string) contract.Call {
	a := map[string][]string{"kit-version": {"kv-1"}, "copies": {copies}}
	for _, x := range extra {
		a[x] = []string{"true"}
	}
	return contract.Call{Words: []string{"kit", "confirm"}, Args: a}
}

func kitState(t *testing.T, e *kenv) (confirmed, checked any) {
	t.Helper()
	raw := e.st.Items["kit"]
	return jsonField(t, raw, "confirmed_version"), jsonField(t, raw, "location_checked_at")
}

func TestContractKeysKitConfirm(t *testing.T) {
	e, clk := kitEnv()
	r, err := e.k.KitConfirm(ctx, kitCall("2"))
	harness.Shape(t, []string{"kit", "confirm"}, r, err)
	want := kitT0.Format("2006-01-02T15:04:05Z")
	if got := jsonField(t, r.Result, "location_checked_at"); got != want {
		t.Errorf("location_checked_at = %v (時計の今 %s を期待)", got, want)
	}
	if c, l := kitState(t, e); c != "kv-1" || l != want {
		t.Errorf("状態の kit: confirmed_version=%v location_checked_at=%v", c, l)
	}
	if e.st.Locked {
		t.Error("状態ファイルの排他を解放していない")
	}
	// 同じ版の 2 回目は上書きしない。
	clk.Advance(48 * time.Hour)
	r, err = e.k.KitConfirm(ctx, kitCall("2"))
	harness.Shape(t, []string{"kit", "confirm"}, r, err)
	if got := jsonField(t, r.Result, "location_checked_at"); got != want {
		t.Errorf("同じ版の再確認で location_checked_at が変わった: %v", got)
	}
	if _, l := kitState(t, e); l != want {
		t.Errorf("同じ版の再確認で状態が上書きされた: %v", l)
	}
	// --recheck は呼ぶたびに更新する。確認した版は変えない。
	later := clk.T.Format("2006-01-02T15:04:05Z")
	r, err = e.k.KitConfirm(ctx, kitCall("2", "recheck"))
	harness.Shape(t, []string{"kit", "confirm"}, r, err)
	if got := jsonField(t, r.Result, "location_checked_at"); got != later {
		t.Errorf("--recheck: location_checked_at = %v (%s を期待)", got, later)
	}
	if c, l := kitState(t, e); c != "kv-1" || l != later {
		t.Errorf("--recheck の後の状態: confirmed_version=%v location_checked_at=%v", c, l)
	}
}

func TestContractKeysKitConfirmCopies(t *testing.T) {
	for _, n := range []string{"1", "3", "0", "x"} {
		e, _ := kitEnv()
		_, err := e.k.KitConfirm(ctx, kitCall(n))
		harness.ExpectCode(t, err, contract.CodeArgInvalid)
		if c, _ := kitState(t, e); c != nil {
			t.Errorf("--copies %s: 2 か所でないのに保管を記録した", n)
		}
	}
}

// --- doctor ---

// offClock は実時刻に offset を足した時計(試験ビルドの「Date + オフセット」。contract.Offsetter を実装する)。
type offClock struct{ off time.Duration }

func (c offClock) Now(context.Context) (time.Time, error) {
	return time.Now().UTC().Add(c.off).Truncate(time.Second), nil
}
func (c offClock) Offset() (time.Duration, error) { return c.off, nil }

// plainClock はオフセットを示さない時計(製品ビルド)。
type plainClock struct{ off time.Duration }

func (c plainClock) Now(context.Context) (time.Time, error) {
	return time.Now().UTC().Add(c.off).Truncate(time.Second), nil
}

type chk struct {
	Name     string          `json:"name"`
	Severity string          `json:"severity"`
	OK       bool            `json:"ok"`
	Next     []any           `json:"next"`
	Count    *int            `json:"count"`
	Raw      json.RawMessage `json:"-"`
}

func doctor(t *testing.T, e *kenv) (stage string, passed bool, cs []chk) {
	t.Helper()
	r, err := e.k.Doctor(ctx, contract.Call{Words: []string{"doctor"}})
	harness.Shape(t, []string{"doctor"}, r, err)
	var res struct {
		Stage  string `json:"stage"`
		Passed bool   `json:"passed"`
		Checks []chk  `json:"checks"`
	}
	harness.Result(t, r, &res)
	return res.Stage, res.Passed, res.Checks
}

func byName(cs []chk, n string) *chk {
	for i := range cs {
		if cs[i].Name == n {
			return &cs[i]
		}
	}
	return nil
}

func names(cs []chk) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

// 段階ごとの検査項目(表の順。advisory は全段階)。
var bootstrapChecks = []string{"service_user", "state_dir_fs", "container_runtime", "check_images", "timer_permission", "listening_ports", "directory_join", "admin_access_closed"}

func TestContractKeysDoctorBootstrap(t *testing.T) {
	e := newEnv() // 定義の候補が無い = bootstrap
	stage, passed, cs := doctor(t, e)
	if stage != "bootstrap" {
		t.Errorf("stage = %q (候補が無いので bootstrap)", stage)
	}
	want := append(slices.Clone(bootstrapChecks), "adapter_dir_modes", "disk_encryption", "swap_disabled")
	if !slices.Equal(names(cs), want) {
		t.Errorf("checks[] = %v (表の順で bootstrap までと advisory)", names(cs))
	}
	checkCommon(t, passed, cs)
}

func checkCommon(t *testing.T, passed bool, cs []chk) {
	t.Helper()
	all := true
	for _, c := range cs {
		if (c.Name == "disk_encryption" || c.Name == "swap_disabled") != (c.Severity == "advisory") {
			t.Errorf("%s の severity = %s", c.Name, c.Severity)
		}
		if !c.OK && len(c.Next) == 0 && c.Severity == "required" {
			t.Errorf("%s が外れているのに next[] が空", c.Name)
		}
		if c.OK && len(c.Next) != 0 {
			t.Errorf("%s は ok なのに next[] がある", c.Name)
		}
		if c.Severity == "required" && !c.OK {
			all = false
		}
	}
	if passed != all {
		t.Errorf("passed = %v (required がすべて ok なら true: %v)", passed, all)
	}
	// check_images はイテレーション 1 では対象 0 件で合格し、count: 0 を示す(検査を飛ばしたように見せない)。他の要素は count を持たない。
	ci := byName(cs, "check_images")
	if ci == nil || !ci.OK || ci.Count == nil || *ci.Count != 0 {
		t.Errorf("check_images は ok: true・count: 0: %+v", ci)
	}
	for _, c := range cs {
		if c.Name != "check_images" && c.Count != nil {
			t.Errorf("%s が count を持つ", c.Name)
		}
	}
}

func TestContractKeysDoctorTimeSkew(t *testing.T) {
	skew := contract.TimeSkewMax + time.Hour
	cases := []struct {
		name  string
		clock contract.Clock
		ok    bool
	}{
		{"時計が実時刻と同じ", plainClock{}, true},
		{"オフセットを与えた時計(Date + オフセットで判定する)", offClock{off: 24 * time.Hour}, true},
		{"オフセットを示さない時計が進んでいる", plainClock{off: skew}, false},
		{"オフセットのある時計が、オフセットの外でも進んでいる", skewedOff{offClock{off: 24 * time.Hour}, skew}, false},
	}
	for _, c := range cases {
		e := newEnv()
		e.st.cand = true
		e.k.Clock = c.clock
		stage, passed, cs := doctor(t, e)
		if stage != "defined" {
			t.Errorf("%s: stage = %q (候補があるので defined)", c.name, stage)
		}
		want := append(slices.Clone(bootstrapChecks), "storage_reachable", "time_skew", "adapter_dir_modes", "disk_encryption", "swap_disabled")
		if !slices.Equal(names(cs), want) {
			t.Errorf("%s: checks[] = %v", c.name, names(cs))
		}
		checkCommon(t, passed, cs)
		ts := byName(cs, "time_skew")
		if ts == nil || ts.OK != c.ok || ts.Severity != "required" {
			t.Errorf("%s: time_skew = %+v (ok = %v を期待)", c.name, ts, c.ok)
		} else if !c.ok && passed {
			t.Errorf("%s: required の time_skew が外れているのに passed", c.name)
		}
	}
}

// skewedOff はオフセットに加えて、実時刻からさらに extra だけ進んだ時計。
type skewedOff struct {
	offClock
	extra time.Duration
}

func (c skewedOff) Now(ctx context.Context) (time.Time, error) {
	t, err := c.offClock.Now(ctx)
	return t.Add(c.extra), err
}
