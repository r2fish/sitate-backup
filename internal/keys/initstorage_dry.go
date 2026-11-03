package keys

// init-storage --dry-run(spec 3.1.1 の dry-run の行、3.2.6。D-0165)。導入の手順 2 は定義を組み立てる前に呼ぶので、候補を読まず、
// 引数(--endpoint・--bucket・--prefix・--adapter・--admin-ref)から plan を作る。保管先準備アダプタの check(枠 admin)だけを呼び、
// バケットも鍵も作らず、状態も秘密ファイルも保管先も動かさない。check の拒否は plan.errors[] に写す。

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type planErr struct {
	Code   contract.Code   `json:"code"`
	Detail json.RawMessage `json:"detail"`
}

func (k *Keys) initStorageDryRun(ctx context.Context, c contract.Call) (contract.Reply, error) {
	endpoint, _ := argOne(c, "endpoint")
	bucket, _ := argOne(c, "bucket")
	prefix, _ := argOne(c, "prefix")
	if endpoint == "" || bucket == "" {
		return contract.Reply{}, errArg("--endpoint", "missing")
	}
	name := k.provisionName(c)
	adminRef, ok := argOne(c, contract.ArgAdminRef)
	if !ok || adminRef == "" {
		adminRef = adminRefDefault
	}
	errs := []planErr{}
	// 結果の設定の欄は、バケットがあれば check の値、無い・読めないときは実行すれば作る設定(要件)を示す。
	cfg := contract.BucketConfig{Versioning: true, LockMode: wantLockMode, DefaultRetention: wantDays}
	if raw, has := c.Secrets[adminRef]; !has {
		errs = append(errs, planErr{contract.CodeSecretMissing, mustRaw(map[string]string{"ref": adminRef})})
	} else {
		got, cerr := checkAdminConfig(ctx, k.Provision, name, endpoint, bucket, prefix, contract.NewSecret(raw))
		switch {
		case cerr == nil:
			cfg = got
		case bucketAbsent(cerr): // 実行すれば作る
		default:
			errs = append(errs, toPlanErr(cerr))
		}
	}
	plan := map[string]any{"reversible": false, "errors": errs, "reissue": argFlag(c, contract.ArgReissue),
		"bucket": map[string]string{"endpoint": endpoint, "bucket": bucket, "prefix": prefix}}
	return reply(map[string]any{"created": false, "lock_mode": cfg.LockMode, "versioning": cfg.Versioning, "default_retention_days": cfg.DefaultRetention,
		"keys": []keyEl{}, "revoked": []map[string]string{}, "permissions": []any{}, "plan": plan})
}

// checkAdminConfig は枠 admin の check の設定(versioning・lock_mode・default_retention_days)を返す。
func checkAdminConfig(ctx context.Context, run contract.AdapterRunner, name, endpoint, bucket, prefix string, admin *contract.Secret) (contract.BucketConfig, error) {
	out, err := provisionCall(ctx, run, name, map[string]any{"op": "check", "endpoint": endpoint, "bucket": bucket, "prefix": prefix, "key_role": nil, "secrets": slotSecrets("admin", admin)})
	if err != nil {
		return contract.BucketConfig{}, err
	}
	var res struct {
		Versioning       bool   `json:"versioning"`
		LockMode         string `json:"lock_mode"`
		DefaultRetention int    `json:"default_retention_days"`
	}
	if json.Unmarshal(out, &res) != nil {
		return contract.BucketConfig{}, errInternal()
	}
	return contract.BucketConfig{Versioning: res.Versioning, LockMode: res.LockMode, DefaultRetention: res.DefaultRetention}, nil
}

// bucketAbsent は check の誤りが「バケットがまだ無い」(アダプタの BUCKET_NOT_OWNED、detail.reason: absent)か。
func bucketAbsent(err error) bool {
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Adapter == nil || ce.Adapter.Code != "BUCKET_NOT_OWNED" {
		return false
	}
	var d struct {
		Reason string `json:"reason"`
	}
	return json.Unmarshal(ce.Adapter.Detail, &d) == nil && d.Reason == "absent"
}

func toPlanErr(err error) planErr {
	var ce *contract.Error
	if errors.As(err, &ce) {
		d := ce.Detail
		if len(d) == 0 {
			d = json.RawMessage("null")
		}
		return planErr{ce.Code, d}
	}
	return planErr{contract.CodeInternal, json.RawMessage("null")}
}

func mustRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
