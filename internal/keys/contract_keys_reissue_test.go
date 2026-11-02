package keys

// init-storage の --reissue と ALREADY_INITIALIZED の契約試験(spec 3.1.2 `init-storage`、3.2.6 の「鍵の再発行と失効の組」)。
// 判定する内容: --reissue は常に発行し直し、bucket check に合格した後で旧鍵だけを失効させる / 合格しなければ旧鍵を失効させない /
// 既に準備済みなら作成・発行を呼ばず ALREADY_INITIALIZED を警告で返す(管理用鍵を受けたときも作成・発行は飛ばし、照合は行う) / 設定が食い違えば BUCKET_MISMATCH。

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// ordKS は失効の時点で bucket check の試し(Try)が何回済んでいたかを記録する。
type ordKS struct {
	*recKS
	pr       *recProbe
	st       *stateT
	atRevoke map[string]int
}

// IssueKeys は発行した鍵を台帳(issued_key)に記録する(実物の発行の振る舞い。spec 4.5)。
func (o *ordKS) IssueKeys(c context.Context, req contract.IssueRequest) ([]contract.IssuedKey, []string, error) {
	iks, orph, err := o.recKS.IssueKeys(c, req)
	for _, ik := range iks {
		o.st.Ledger = append(o.st.Ledger, json.RawMessage(`{"kind":"issued_key","key_id":"`+ik.KeyID+`","role":"`+ik.Role+`"}`))
	}
	return iks, orph, err
}

func (o *ordKS) RevokeKey(c context.Context, req contract.RevokeRequest) ([]string, []string, error) {
	o.pr.mu.Lock()
	n := len(o.pr.Tried)
	o.pr.mu.Unlock()
	for _, id := range req.KeyIDs {
		o.atRevoke[id] = n
	}
	return o.recKS.RevokeKey(c, req)
}

// issuedSecrets は保管先の鍵が既に秘密ファイルにある状態の偽の Secrets(参照名は何でも storage_keys を返す)。
type issuedSecrets struct{}

func (issuedSecrets) Get(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`{"kind":"storage_keys","keys":[{"role":"write","key_id":"K1","secret":"S1"},{"role":"read","key_id":"K2","secret":"S2"}]}`), nil
}
func (issuedSecrets) Put(context.Context, string, json.RawMessage) error { return nil }
func (issuedSecrets) Delete(context.Context, string) error               { return nil }

const reissueCheck = `{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[` +
	`{"role":"write","key_id":"K1","prefix":null,"capabilities":[]},{"role":"read","key_id":"K2","prefix":null,"capabilities":[]},` +
	`{"role":"write","key_id":"kid-write","prefix":null,"capabilities":[]},{"role":"read","key_id":"kid-read","prefix":null,"capabilities":[]}]}`

func reissueEnv() (*kenv, *ordKS) {
	e := initEnv(reissueCheck)
	e.st.Ledger = append(e.st.Ledger,
		json.RawMessage(`{"kind":"issued_key","key_id":"K1","role":"write"}`), json.RawMessage(`{"kind":"issued_key","key_id":"K2","role":"read"}`))
	o := &ordKS{recKS: e.ks, pr: e.pr, st: e.st, atRevoke: map[string]int{}}
	e.k.KeyS = o
	e.k.Secrets = issuedSecrets{}
	return e, o
}

func reissueCall() contract.Call {
	c := initCall()
	c.Args["reissue"] = []string{}
	return c
}

func TestContractKeysInitStorageReissue(t *testing.T) {
	e, o := reissueEnv()
	r, err := e.k.InitStorage(ctx, reissueCall())
	harness.Shape(t, []string{"init-storage"}, r, err)
	if !slices.Equal(e.ks.Issued, []string{"write", "read"}) {
		t.Errorf("--reissue は準備済みでも常に発行し直す: %v", e.ks.Issued)
	}
	slices.Sort(e.ks.Revoked)
	if !slices.Equal(e.ks.Revoked, []string{"K1", "K2"}) {
		t.Errorf("失効した鍵 = %v (旧鍵 K1・K2 だけ。今回発行した鍵は失効しない)", e.ks.Revoked)
	}
	var res struct {
		Keys []struct {
			Role  string `json:"role"`
			KeyID string `json:"key_id"`
		} `json:"keys"`
		Revoked []struct {
			KeyID string `json:"key_id"`
		} `json:"revoked"`
	}
	harness.Result(t, r, &res)
	var rev []string
	for _, x := range res.Revoked {
		rev = append(rev, x.KeyID)
	}
	slices.Sort(rev)
	if !slices.Equal(rev, []string{"K1", "K2"}) {
		t.Errorf("revoked[] = %v (K1・K2)", rev)
	}
	if len(res.Keys) != 2 {
		t.Errorf("keys[] は新しい鍵 2 つ: %s", r.Result)
	}
	for _, id := range []string{"K1", "K2"} {
		if o.atRevoke[id] == 0 {
			t.Errorf("%s を bucket check(試し)の前に失効させた", id)
		}
	}
	if e.run.called("create-bucket") > 1 {
		t.Errorf("バケットの作成は多くて 1 回: %d", e.run.called("create-bucket"))
	}
}

func TestContractKeysInitStorageReissueCheckFails(t *testing.T) {
	// 新しい鍵が bucket check に合格しない(書き込み鍵が保持を短縮できる)なら、旧鍵は失効させない。
	e, _ := reissueEnv()
	e.pr.Allowed["write/retention_shorten"] = true
	_, err := e.k.InitStorage(ctx, reissueCall())
	harness.Implemented(t, err)
	if len(e.ks.Revoked) != 0 {
		t.Errorf("bucket check に合格していないのに旧鍵を失効させた: %v", e.ks.Revoked)
	}
}

func alreadyEnv() *kenv {
	e := newEnv()
	e.st.cand, e.st.candDef = true, candStorage
	goodBucket(e)
	core := fake.NewCore(&fake.ManualClock{T: fake.StartTime})
	if err := core.CreateBucket("b", true); err != nil {
		panic(err)
	}
	_ = core.SetVersioning("b", true)
	_ = core.SetDefaultRetention("b", 90)
	e.k.Store = &fake.ObjectStore{Core: core, Bucket: "b", Role: fake.RoleWrite, Params: contract.FixedRetention{}}
	e.k.Secrets = issuedSecrets{}
	e.run.Resp["check:admin"] = ok(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`)
	return e
}

func TestContractKeysInitStorageAlreadyInitialized(t *testing.T) {
	e := alreadyEnv()
	e.pr.Allowed["write/put"] = true
	// 管理用鍵なし: アダプタを一切呼ばず、何も変えず、成功と警告。
	c := initCall()
	delete(c.Args, "admin-ref")
	c.Secrets = nil
	r, err := e.k.InitStorage(ctx, c)
	harness.Shape(t, []string{"init-storage"}, r, err)
	if findWarn(r, contract.CodeAlreadyInitialized) == nil {
		t.Errorf("警告 ALREADY_INITIALIZED が無い: %+v", r.Warnings)
	}
	// bucket check の鍵の権限の読み取り(check:write・check:read)は呼んでよい。作成・発行・失効・管理用鍵の照合は呼ばない。
	if n := e.run.called("create-bucket") + e.run.called("issue-keys") + e.run.called("revoke-keys") + e.run.called("check:admin"); n != 0 || len(e.ks.Issued) != 0 || len(e.ks.Revoked) != 0 {
		t.Errorf("管理用鍵なしで作成・発行・失効・照合を呼んだ: %d issued=%v revoked=%v", n, e.ks.Issued, e.ks.Revoked)
	}
	if jsonField(t, r.Result, "created") != false {
		t.Errorf("created = %v", jsonField(t, r.Result, "created"))
	}
	// 管理用鍵あり: 作成・発行は飛ばし、発行の後の照合(check:admin)は行う。
	e2 := alreadyEnv()
	r2, err := e2.k.InitStorage(ctx, initCall())
	harness.Shape(t, []string{"init-storage"}, r2, err)
	if findWarn(r2, contract.CodeAlreadyInitialized) == nil {
		t.Errorf("管理用鍵ありでも警告 ALREADY_INITIALIZED: %+v", r2.Warnings)
	}
	if e2.run.called("create-bucket") != 0 || e2.run.called("issue-keys") != 0 || len(e2.ks.Issued) != 0 || e2.run.called("check:admin") < 1 {
		t.Errorf("作成・発行は飛ばし照合は行う: create=%d issue=%d issued=%v check:admin=%d", e2.run.called("create-bucket"), e2.run.called("issue-keys"), e2.ks.Issued, e2.run.called("check:admin"))
	}
}

func TestContractKeysInitStorageBucketMismatch(t *testing.T) {
	// 発行済みの鍵があり、バケットの設定が食い違う(既定の保持が 90 日でない)なら、作成・発行へ進まず BUCKET_MISMATCH。
	e := alreadyEnv()
	core := fake.NewCore(&fake.ManualClock{T: fake.StartTime})
	if err := core.CreateBucket("b", true); err != nil {
		t.Fatal(err)
	}
	_ = core.SetVersioning("b", true)
	_ = core.SetDefaultRetention("b", 30)
	e.k.Store = &fake.ObjectStore{Core: core, Bucket: "b", Role: fake.RoleWrite, Params: contract.FixedRetention{}}
	_, err := e.k.InitStorage(ctx, initCall())
	harness.Implemented(t, err)
	harness.ExpectCode(t, err, contract.CodeBucketMismatch)
	if e.run.called("create-bucket") != 0 || e.run.called("issue-keys") != 0 || len(e.ks.Issued) != 0 {
		t.Errorf("食い違いで作成・発行へ進んだ")
	}
}
