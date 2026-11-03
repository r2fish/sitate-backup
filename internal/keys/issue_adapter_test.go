package keys

// 主経路の第 2 段で見つけた誤り E2 の試験: init-storage の --adapter は、バケット作成・権限の読み取りだけでなく鍵の発行・失効にも効く
// (KeyService.IssueKeys は契約の IssueRequest にアダプタ名が無く、既定の b2 を呼んでいた)。

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

// 実物の KeyService は、init-storage が文脈に載せたアダプタ名で issue-keys を呼ぶ。
func TestKeyServiceIssueKeysCallsTheAdapterNamedOnTheContext(t *testing.T) {
	r := fake.NewAdapterRunner()
	r.Script("provision", fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"contract":"sitate.provision/v1","issued":[{"role":"write","key_id":"K2","secret":"S2"},{"role":"read","key_id":"K3","secret":"S3"}]}`)})
	if _, _, err := provisionKS(r).IssueKeys(withProvision(context.Background(), "other"), issueReq("write", "read")); err != nil {
		t.Fatal(err)
	}
	if len(r.Calls) != 1 || r.Calls[0].Name != "other" {
		t.Fatalf("文脈のアダプタ名で呼ぶはず: %+v", r.Calls)
	}
}

// 文脈に名前が無ければ、結線で渡された名前(既定)を使う。
func TestKeyServiceIssueKeysFallsBackToConfiguredName(t *testing.T) {
	r := fake.NewAdapterRunner()
	r.Script("provision", fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"contract":"sitate.provision/v1","issued":[{"role":"write","key_id":"K2","secret":"S2"}]}`)})
	if _, _, err := provisionKS(r).IssueKeys(ctx, issueReq("write")); err != nil {
		t.Fatal(err)
	}
	if len(r.Calls) != 1 || r.Calls[0].Name != "b2" {
		t.Fatalf("既定の名前で呼ぶはず: %+v", r.Calls)
	}
}

// ctxKS は IssueKeys が受けた文脈のアダプタ名を記録する。
type ctxKS struct {
	*recKS
	seen []string
}

func (c *ctxKS) IssueKeys(cx context.Context, req contract.IssueRequest) ([]contract.IssuedKey, []string, error) {
	c.seen = append(c.seen, provisionNameFrom(cx))
	return c.recKS.IssueKeys(cx, req)
}

// init-storage は --adapter の名前を文脈に載せて KeyService に渡す。
func TestInitStoragePassesAdapterNameToKeyService(t *testing.T) {
	e := initEnv(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`)
	w := &ctxKS{recKS: e.ks}
	e.k.KeyS = w
	call := initCall()
	call.Args["adapter"] = []string{"other"}
	if _, err := e.k.InitStorage(ctx, call); err != nil {
		t.Fatal(err)
	}
	if len(w.seen) != 1 || w.seen[0] != "other" {
		t.Fatalf("IssueKeys が受けた文脈のアダプタ名 = %v", w.seen)
	}
}
