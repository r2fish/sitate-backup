package keys

// bucket check と init-storage の契約試験(作業指示書 5.7 の完了条件): 結果の中身を判定する。
// 偽の保管先(internal/contract/fake)と、操作ごとに応答する偽の保管先準備アダプタに対して呼び、
// 結果は封筒のスキーマ(cli/results.json の定義)でも検証する。

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// permRow は 3.2.6 の操作の表の 1 行(書き込み鍵・読み取り鍵の期待。"" はその鍵の要素を作らない)と、判定のしかた(試す / 権限の読み取り)。
type permRow struct {
	op, write, read string
	tryW, tryR      bool
}

var permTable = []permRow{
	{"put", "allow", "deny", true, false},
	{"list_versions", "allow", "allow", true, true},
	{"get_version", "allow", "allow", true, true},
	{"get_retention", "allow", "", true, false},
	{"get_bucket_config", "allow", "", true, false},
	{"extend_retention", "allow", "deny", false, false},
	{"delete_version", "allow", "deny", false, false},
	{"retention_shorten", "deny", "deny", true, true},
	{"unversioned_delete", "deny", "deny", true, true},
	{"bucket_policy", "deny", "deny", false, false},
	{"lifecycle", "deny", "deny", false, false},
	{"default_retention", "deny", "deny", false, false},
	{"governance_bypass", "deny", "deny", false, false},
	{"legal_hold", "deny", "deny", true, true},
	{"bucket_delete", "deny", "deny", false, false},
	{"key_manage", "deny", "deny", false, false},
}

func (r permRow) exp(role string) string {
	if role == "write" {
		return r.write
	}
	return r.read
}

func (r permRow) tried(role string) bool {
	if role == "write" {
		return r.tryW
	}
	return r.tryR
}

// capsOf は role の鍵の権限として読める操作(権限の読み取りで判定する操作のうち許すもの)。
func capsOf(role string) []string {
	out := []string{}
	for _, r := range permTable {
		if !r.tried(role) && r.exp(role) == "allow" {
			out = append(out, r.op)
		}
	}
	return out
}

func keyFrame(role string, caps []string) func() (json.RawMessage, error) {
	c := "null"
	if caps != nil {
		b, _ := json.Marshal(caps)
		c = string(b)
	}
	return ok(fmt.Sprintf(`{"versioning":null,"lock_mode":null,"default_retention_days":null,"keys":[{"role":%q,"key_id":"K-%s","prefix":null,"capabilities":%s}]}`, role, role, c))
}

// goodBucket は仕様どおり(禁止すべき操作がすべて拒否される)の保管先と鍵を env に置く。
func goodBucket(e *kenv) {
	e.pr.Allowed = map[string]bool{}
	for _, role := range []string{"write", "read"} {
		for _, r := range permTable {
			if r.tried(role) && r.exp(role) == "allow" {
				e.pr.Allowed[role+"/"+r.op] = true
			}
		}
		e.run.Resp["check:"+role] = keyFrame(role, capsOf(role))
	}
}

func bucketCall() contract.Call {
	return contract.Call{Words: []string{"bucket", "check"}, Args: map[string][]string{"key-ref": {"store-main"}}, SecretOut: true}
}

type permEl struct {
	Op      string `json:"op"`
	KeyRole string `json:"key_role"`
	Expect  string `json:"expected"`
	Actual  string `json:"actual"`
}

func perms(t *testing.T, r contract.Reply) []permEl {
	t.Helper()
	var res struct {
		Permissions []permEl `json:"permissions"`
	}
	harness.Result(t, r, &res)
	return res.Permissions
}

func findPerm(ps []permEl, op, role string) *permEl {
	for i := range ps {
		if ps[i].Op == op && ps[i].KeyRole == role {
			return &ps[i]
		}
	}
	return nil
}

// wantPermissions は permissions[] が表の全組(— を除く)を、expected が表どおり・actual が expected と一致で持つことを確かめる。
func wantPermissions(t *testing.T, ps []permEl) {
	t.Helper()
	n := 0
	for _, row := range permTable {
		for _, role := range []string{"write", "read"} {
			if row.exp(role) == "" {
				if findPerm(ps, row.op, role) != nil {
					t.Errorf("表で — の組 %s/%s に要素がある", role, row.op)
				}
				continue
			}
			n++
			p := findPerm(ps, row.op, role)
			if p == nil {
				t.Errorf("permissions[] に %s/%s が無い", role, row.op)
			} else if p.Expect != row.exp(role) || p.Actual != p.Expect {
				t.Errorf("%s/%s: expected=%q actual=%q (表は %q)", role, row.op, p.Expect, p.Actual, row.exp(role))
			}
		}
	}
	if len(ps) != n {
		t.Errorf("permissions[] は %d 件のはずが %d 件", n, len(ps))
	}
}

func TestContractKeysBucketCheck(t *testing.T) {
	e := newEnv()
	goodBucket(e)
	r, err := e.k.BucketCheck(ctx, bucketCall())
	harness.Shape(t, []string{"bucket", "check"}, r, err)
	if jsonField(t, r.Result, "lock_mode") != "COMPLIANCE" || jsonField(t, r.Result, "versioning") != true {
		t.Errorf("lock_mode・versioning が保管先の設定と違う: %s", r.Result)
	}
	wantPermissions(t, perms(t, r))
	if len(r.Warnings) != 0 {
		t.Errorf("健全な設定で警告が出た(版指定なしの削除を許さない設定では KEY_ALLOWS_UNVERSIONED_DELETE を返さない): %+v", r.Warnings)
	}
	if len(e.pr.Puts) == 0 || !strings.Contains(e.pr.Puts[0], "probe/") {
		t.Errorf("試験用オブジェクトを probe/ の下へ書いていない: %v", e.pr.Puts)
	}
	if len(e.pr.NoIntent) != 0 {
		t.Errorf("probe/ への書き込みの前に write_intent を実行の記録に残していない: %v", e.pr.NoIntent)
	}
	for _, role := range []string{"write", "read"} {
		if e.run.called("check:"+role) != 1 {
			t.Errorf("%s 鍵の権限の読み取り(check を枠 key で)が %d 回", role, e.run.called("check:"+role))
		}
	}
}

func TestContractKeysBucketCheckUnversionedDelete(t *testing.T) {
	e := newEnv()
	goodBucket(e)
	e.pr.Allowed["write/unversioned_delete"] = true // 版指定なしの削除を許す設定(B2 の書き込み鍵)
	r, err := e.k.BucketCheck(ctx, bucketCall())
	harness.Shape(t, []string{"bucket", "check"}, r, err)
	w := findWarn(r, contract.CodeKeyAllowsUnversionedDelete)
	if w == nil || jsonField(t, w.Detail, "role") != "write" {
		t.Fatalf("KEY_ALLOWS_UNVERSIONED_DELETE(detail.role = write)の警告が無い: %+v", r.Warnings)
	}
	if len(r.Warnings) != 1 {
		t.Errorf("警告はこの 1 件だけのはず: %+v", r.Warnings)
	}
	if p := findPerm(perms(t, r), "unversioned_delete", "write"); p == nil || p.Actual != "allow" || p.Expect != "deny" {
		t.Errorf("通った事実は permissions[] に actual=allow で残す: %+v", p)
	}
}

func TestContractKeysBucketCheckLegalHold(t *testing.T) {
	e := newEnv()
	goodBucket(e)
	e.pr.Allowed["write/legal_hold"] = true
	r, err := e.k.BucketCheck(ctx, bucketCall())
	harness.Shape(t, []string{"bucket", "check"}, r, err)
	if findWarn(r, contract.CodeKeyLegalHoldAllowed) == nil || len(r.Warnings) != 1 {
		t.Errorf("KEY_LEGAL_HOLD_ALLOWED の警告 1 件を期待: %+v", r.Warnings)
	}
}

func TestContractKeysBucketCheckTooPowerful(t *testing.T) {
	cases := []struct {
		name, role string
		ops        []string
		tweak      func(*kenv)
	}{
		{"書き込み鍵が保持を短縮できる", "write", []string{"retention_shorten"}, func(e *kenv) { e.pr.Allowed["write/retention_shorten"] = true }},
		{"読み取り鍵が書ける(権限の読み取り)", "read", []string{"put"}, func(e *kenv) {
			e.run.Resp["check:read"] = keyFrame("read", append(capsOf("read"), "put"))
		}},
		{"書き込み鍵がバケットのポリシーを変えられる", "write", []string{"bucket_policy"}, func(e *kenv) {
			e.run.Resp["check:write"] = keyFrame("write", append(capsOf("write"), "bucket_policy"))
		}},
	}
	for _, c := range cases {
		e := newEnv()
		goodBucket(e)
		c.tweak(e)
		_, err := e.k.BucketCheck(ctx, bucketCall())
		ce := harness.ExpectCode(t, err, contract.CodeKeyTooPowerful)
		var d struct {
			Role string   `json:"role"`
			Ops  []string `json:"ops"`
		}
		_ = json.Unmarshal(ce.Detail, &d)
		if d.Role != c.role || !slices.Equal(d.Ops, c.ops) {
			t.Errorf("%s: detail = %s (role %s ops %v を期待)", c.name, ce.Detail, c.role, c.ops)
		}
	}
}

func TestContractKeysBucketCheckUntested(t *testing.T) {
	e := newEnv()
	goodBucket(e)
	e.run.Resp["check:write"] = keyFrame("write", nil) // 鍵が自分の権限を読めない事業者(capabilities が null)
	r, err := e.k.BucketCheck(ctx, bucketCall())
	harness.Shape(t, []string{"bucket", "check"}, r, err)
	for _, op := range []string{"extend_retention", "delete_version", "bucket_policy", "key_manage"} {
		if p := findPerm(perms(t, r), op, "write"); p == nil || p.Actual != "untested" {
			t.Errorf("write/%s: 権限を読めないときは actual=untested(不合格にしない): %+v", op, p)
		}
	}
	if p := findPerm(perms(t, r), "retention_shorten", "write"); p == nil || p.Actual != "deny" {
		t.Errorf("試す操作は untested にならない: %+v", p)
	}
}

// --- init-storage ---

func initCall() contract.Call {
	return contract.Call{Words: []string{"init-storage"}, SecretOut: true,
		Args:    map[string][]string{"endpoint": {"https://s3.example.invalid"}, "bucket": {"bkt-1"}, "prefix": {"sitate/"}, "adapter": {"b2"}, "admin-ref": {"admin"}},
		Secrets: contract.SecretInput{"admin": json.RawMessage(`{"key_id":"ADMIN-ID","secret":"ADMIN-SECRET-VALUE"}`)}}
}

func initEnv(adminCheck string) *kenv {
	e := newEnv()
	e.st.cand, e.st.candDef = true, candStorage // def validate(手順 4)が init-storage(手順 5)より先: 候補の storage[0] が参照名を決める
	e.k.Secrets = &memSecrets{}
	goodBucket(e)
	e.run.Resp["create-bucket"] = ok(`{"created":true,"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90}`)
	e.run.Resp["check:admin"] = ok(adminCheck)
	return e
}

func TestContractKeysInitStorage(t *testing.T) {
	e := initEnv(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`)
	r, err := e.k.InitStorage(ctx, initCall())
	harness.Shape(t, []string{"init-storage"}, r, err)
	if jsonField(t, r.Result, "created") != true || jsonField(t, r.Result, "lock_mode") != "COMPLIANCE" ||
		jsonField(t, r.Result, "versioning") != true || jsonField(t, r.Result, "default_retention_days") != float64(90) {
		t.Errorf("バケットの結果がアダプタの応答と違う: %s", r.Result)
	}
	var res struct {
		Keys []struct {
			Role   string `json:"role"`
			KeyRef string `json:"key_ref"`
			KeyID  string `json:"key_id"`
		} `json:"keys"`
		Rev []any `json:"revoked"`
	}
	harness.Result(t, r, &res)
	if len(res.Keys) != 2 || res.Keys[0].Role != "write" || res.Keys[0].KeyID != "kid-write" || res.Keys[1].Role != "read" || res.Keys[1].KeyID != "kid-read" || res.Keys[0].KeyRef == "" || res.Keys[1].KeyRef == "" {
		t.Errorf("keys[] は 書き込み・読み取りの順で、発行した key_id と参照名を持つ: %s", r.Result)
	}
	if len(res.Rev) != 0 {
		t.Errorf("--reissue でないので失効は無い: %s", r.Result)
	}
	if !slices.Equal(e.ks.Issued, []string{"write", "read"}) {
		t.Errorf("発行した役割 = %v", e.ks.Issued)
	}
	wantPermissions(t, perms(t, r))
	if e.run.called("create-bucket") != 1 || e.run.called("check:admin") < 1 {
		t.Errorf("バケット作成 1 回と、発行の後の照合(導入の初回でも走る)を期待: create-bucket=%d check:admin=%d", e.run.called("create-bucket"), e.run.called("check:admin"))
	}
	if strings.Contains(string(r.Result), "ADMIN-SECRET-VALUE") {
		t.Error("結果に事業者の管理用鍵の値が出ている")
	}
}

func TestContractKeysInitStorageOrphan(t *testing.T) {
	// 照合の check が、台帳にも今回の結果にも無い製品の印つきの鍵 K9 を返す: 孤児として台帳に載せ、失効する。今回発行した鍵は失効しない。
	e := initEnv(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[` +
		`{"role":"write","key_id":"K9","prefix":null,"capabilities":[]},{"role":"write","key_id":"kid-write","prefix":null,"capabilities":[]},{"role":"read","key_id":"kid-read","prefix":null,"capabilities":[]}]}`)
	r, err := e.k.InitStorage(ctx, initCall())
	harness.Shape(t, []string{"init-storage"}, r, err)
	if !slices.Equal(e.ks.Revoked, []string{"K9"}) {
		t.Errorf("失効した鍵 = %v (K9 だけ)", e.ks.Revoked)
	}
	found := false
	for _, l := range e.st.Ledger {
		if jsonField(t, l, "key_id") == "K9" && jsonField(t, l, "orphaned") == true {
			found = true
		}
	}
	if !found {
		t.Errorf("台帳に K9 が orphaned: true で載っていない: %s", e.st.Ledger)
	}
	if findWarn(r, contract.CodeKeyReconcileUnverified) != nil {
		t.Error("照合できたのに KEY_RECONCILE_UNVERIFIED を返した")
	}
}

func TestContractKeysInitStorageReconcileFails(t *testing.T) {
	// 発行の後の照合の check 自体が失敗: 発行の結果は有効のまま、警告 KEY_RECONCILE_UNVERIFIED(終了 0)。detail.reason にアダプタの原因コード。
	e := initEnv(`{}`)
	e.run.Resp["check:admin"] = adapterFail("PROVIDER_UNREACHABLE")
	r, err := e.k.InitStorage(ctx, initCall())
	harness.Shape(t, []string{"init-storage"}, r, err)
	w := findWarn(r, contract.CodeKeyReconcileUnverified)
	if w == nil {
		t.Fatalf("警告 KEY_RECONCILE_UNVERIFIED が無い: %+v", r.Warnings)
	}
	if reason, _ := jsonField(t, w.Detail, "reason").(string); reason != "PROVIDER_UNREACHABLE" && reason != string(contract.CodeAdapterError) {
		t.Errorf("detail.reason = %v (アダプタの原因コードを期待)", jsonField(t, w.Detail, "reason"))
	}
	if !slices.Equal(e.ks.Issued, []string{"write", "read"}) || len(e.ks.Revoked) != 0 {
		t.Errorf("発行は有効のまま・失効はしない: issued=%v revoked=%v", e.ks.Issued, e.ks.Revoked)
	}
}
