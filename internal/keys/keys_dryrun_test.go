package keys

// まとめレビュー H3: --dry-run は実行しない(spec 3.1.1)。init と init-storage は、状態も保管先もアダプタも鍵の発行も動かさない。

import (
	"encoding/json"
	"errors"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func TestInitDryRunChangesNothing(t *testing.T) {
	e, call := initPhraseEnv()
	useBundle(e, layout())
	call.DryRun = true
	r, err := e.k.Init(ctx, call)
	wantNotImpl(t, err)
	if r.Phrase != nil {
		t.Error("dry-run がフレーズを返した")
	}
	if len(e.st.Items) != 0 {
		t.Errorf("状態が変わった: %v", e.st.Items)
	}
	if _, err := os.Stat(filepath.Join(e.dir, contract.KeysPendingDir)); err == nil {
		t.Error("保留ができた")
	}
}

// 手順 2: 候補の無い状態で、引数だけから plan を作る。アダプタの check(枠 admin)以外は呼ばず、状態も保管先も秘密ファイルも動かさない。
func dryEnv(check func() (json.RawMessage, error)) *kenv {
	e := newEnv()
	e.k.Secrets = &memSecrets{}
	e.run.Resp["check:admin"] = check
	return e
}

func dryPlan(t *testing.T, r contract.Reply) (errs []map[string]any, plan map[string]any) {
	t.Helper()
	var res struct {
		Plan map[string]any `json:"plan"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil || res.Plan == nil {
		t.Fatalf("result.plan が無い: %s", r.Result)
	}
	if arr, ok := res.Plan["errors"].([]any); ok {
		for _, x := range arr {
			errs = append(errs, x.(map[string]any))
		}
	} else {
		t.Fatalf("plan.errors が配列でない: %s", r.Result)
	}
	return errs, res.Plan
}

func TestInitStorageDryRunWithoutCandidate(t *testing.T) {
	cases := map[string]struct {
		check func() (json.RawMessage, error)
		admin bool
		want  string // plan.errors[0].code。空なら errors は空
	}{
		"合格": {check: ok(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`), admin: true},
		"バケットがまだ無い(実行すれば作る)": {check: func() (json.RawMessage, error) {
			return nil, &contract.Error{Code: contract.CodeAdapterError, Class: "adapter", Detail: json.RawMessage(`{"kind":"provision","name":"b2","adapter_code":"BUCKET_NOT_OWNED"}`),
				Adapter: &contract.AdapterError{Code: "BUCKET_NOT_OWNED", Detail: json.RawMessage(`{"reason":"absent"}`)}, Next: []contract.Next{}}
		}, admin: true},
		"管理用鍵の拒否": {check: adapterFail("AUTH_FAILED"), admin: true, want: "ADAPTER_ERROR"},
		"管理用鍵が無い": {check: ok(`{}`), admin: false, want: "SECRET_MISSING"},
	}
	for name, c := range cases {
		e := dryEnv(c.check) // e.st.cand は偽: 候補は無い
		call := initCall()
		call.DryRun = true
		if !c.admin {
			call.Secrets = nil
		}
		r, err := e.k.InitStorage(ctx, call)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		harness.Shape(t, []string{"init-storage"}, r, err) // 結果の形(plan を含む)が契約の結果の schema に合う
		errs, _ := dryPlan(t, r)
		if c.want == "" && len(errs) != 0 || c.want != "" && (len(errs) != 1 || errs[0]["code"] != c.want) {
			t.Errorf("%s: plan.errors = %v", name, errs)
		}
		for _, call := range e.run.Calls {
			if call["_key"] != "check:admin" {
				t.Errorf("%s: check 以外のアダプタ操作 %v", name, call["_key"])
			}
		}
		if len(e.ks.Reqs) != 0 || len(e.st.Items) != 0 {
			t.Errorf("%s: 状態・発行が動いた: %v %v", name, e.ks.Reqs, e.st.Items)
		}
		if _, err := e.k.Secrets.Get(ctx, "store-main"); err == nil {
			t.Errorf("%s: 秘密ファイルを置いた", name)
		}
		if bodies := storeBodies2(t, e); len(bodies) != 0 {
			t.Errorf("%s: 保管先へ書いた", name)
		}
	}
}

func storeBodies2(t *testing.T, e *kenv) []string {
	t.Helper()
	vs, err := e.k.Store.ListVersions(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, v := range vs {
		out = append(out, v.Key)
	}
	return out
}

func wantNotImpl(t *testing.T, err error) {
	t.Helper()
	var ni *contract.NotImplemented
	if !errors.As(err, &ni) {
		t.Fatalf("NOT_IMPLEMENTED を期待したが %v", err)
	}
}

// M1: seal の後でも、承認された storage-init の適用(apply の run 要素。内部の印 from-apply)は通る。直接実行は APPROVAL_REQUIRED のまま。
// 承認済みの定義の storage[0] と違う --endpoint などは ALREADY_INITIALIZED(保管先を替える提案は作らない。3.1.2.1 の分岐 6)。
func TestInitStorageApplyAfterSeal(t *testing.T) {
	const def = `{"version":1,"storage":[{"endpoint":"https://s3.example.invalid","bucket":"bkt-1","prefix":"sitate/","key_ref":"store-main"}]}`
	run := func(mut func(c *contract.Call)) (*kenv, contract.Reply, error) {
		e := initEnv(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`)
		e.st.Items = map[string]json.RawMessage{"sealed": json.RawMessage("true")}
		e.st.Def = []byte(def)
		c := initCall()
		c.Args["reissue"] = []string{}
		mut(&c)
		r, err := e.k.InitStorage(ctx, c)
		return e, r, err
	}
	if _, _, err := run(func(c *contract.Call) {}); err == nil {
		t.Error("直接実行が通った")
	}
	e, _, err := run(func(c *contract.Call) { c.Args["from-apply"] = []string{"true"} })
	if err != nil {
		t.Fatalf("承認された適用が seal の後に失敗: %v", err)
	}
	if len(e.ks.Reqs) == 0 {
		t.Error("鍵を発行していない")
	}
	e2, _, err := run(func(c *contract.Call) {
		c.Args["from-apply"] = []string{"true"}
		c.Args["bucket"] = []string{"attacker-bkt"}
	})
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeAlreadyInitialized {
		t.Fatalf("別のバケットへの適用: %v", err)
	}
	if len(e2.ks.Reqs) != 0 || len(e2.run.Calls) != 0 {
		t.Error("別のバケットへ管理用鍵を渡した")
	}
}
