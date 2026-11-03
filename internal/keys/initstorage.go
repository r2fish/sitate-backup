package keys

// init-storage(spec 3.1.2、3.2.6): バケット作成、書き込み鍵と読み取り鍵の発行、設定の検査、続けて bucket check。
// 先に bucket check して合格していれば作成・発行を飛ばして ALREADY_INITIALIZED(警告)を返す。--reissue は常に発行し直し、bucket check に合格した後で旧鍵だけを失効させる。
// issue-keys の後は成否を問わず check(枠 admin)で台帳に無い印付きの鍵を照らし、孤児として台帳に記録して失効させる(管理用鍵を受けたときだけ)。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const (
	adminRefDefault = "admin"
	// 期待するバケットの設定(spec 4.2。既定保持は日次の保持期間)。
	wantLockMode = "COMPLIANCE"
	wantDays     = contract.RetentionDailyDays
)

var issueRoles = []string{"write", "read"}

// storageKeysJSON は秘密ファイル(kind: storage_keys)の値。
func storageKeysJSON(issued []contract.IssuedKey) ([]byte, error) {
	sk := contract.StorageKeysSecret{Kind: contract.SecretKindStorageKeys}
	for _, i := range issued {
		sk.Keys = append(sk.Keys, contract.StorageKeyEntry{Role: i.Role, KeyID: i.KeyID, Secret: string(i.Secret.Reveal())})
	}
	return json.Marshal(sk)
}

func wipeIssued(iss []contract.IssuedKey) {
	for _, i := range iss {
		if i.Secret != nil {
			i.Secret.Wipe()
		}
	}
}

type keyEl struct {
	Role   string `json:"role"`
	KeyRef string `json:"key_ref"`
	KeyID  string `json:"key_id"`
}

type isCtx struct {
	c            contract.Call
	t            target
	name         string
	admin        *contract.Secret
	warns        []contract.Warning
	ledgerWarned bool
}

// InitStorage は契約 contract.Keys を満たす。
func (k *Keys) InitStorage(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := errNeedDeps(k.Secrets != nil, k.State != nil, k.KeyS != nil, k.Store != nil, k.Probe != nil, k.Provision != nil, k.Recorder != nil, k.Clock != nil); err != nil {
		return contract.Reply{}, err
	}
	// seal の後の init-storage(--reissue・--dry-run を含む)は承認を要する(propose storage-init。spec 3.1.2)。直接実行は拒否する。
	var sealed bool
	if _, err := k.getItem(ctx, "sealed", &sealed); err != nil {
		return contract.Reply{}, err
	}
	if sealed {
		e := mkErr(contract.CodeApprovalRequired, map[string]any{"kind": "storage-init"})
		e.Next = []contract.Next{{Argv: []string{"sitate", "propose", "storage-init"}, RequiresApproval: true}}
		return contract.Reply{}, e
	}
	t, found, err := k.storageTarget(ctx, c)
	if err != nil {
		return contract.Reply{}, err
	}
	if !found {
		return contract.Reply{}, mkErr(contract.CodeDefInvalid, map[string]any{"issues": []map[string]string{{"pointer": "", "reason": "missing"}}})
	}
	if t.KeyRef == "" {
		return contract.Reply{}, mkErr(contract.CodeDefInvalid, map[string]any{"issues": []map[string]string{{"pointer": "/storage/0/key_ref", "reason": "missing"}}})
	}
	ic := &isCtx{c: c, t: t, name: k.provisionName(c)}
	ctx = withProvision(ctx, ic.name) // 鍵の発行・失効(KeyService)も --adapter のアダプタを呼ぶ
	adminRef, ok := argOne(c, contract.ArgAdminRef)
	if !ok || adminRef == "" {
		adminRef = adminRefDefault
	}
	if raw, has := c.Secrets[adminRef]; has {
		// 管理用鍵の持ち主は呼び出し側(cli が Call.Secrets を消す)。ここは参照を渡すだけで消さない(IssueRequest.Admin を契約試験が呼び出しの後に読む)。
		ic.admin = contract.NewSecret(raw)
	}
	reissue := argFlag(c, contract.ArgReissue)
	if !reissue {
		if known, ok := k.existingKeys(ctx, t.KeyRef); ok {
			return k.initStorageExisting(ctx, ic, known)
		}
	}
	if ic.admin == nil {
		return contract.Reply{}, mkErr(contract.CodeSecretMissing, map[string]string{"ref": adminRef})
	}
	return k.initStorageIssue(ctx, ic, reissue)
}

// existingKeys は秘密ファイルに書き込み鍵と読み取り鍵がそろっているか(key_id の列)。
func (k *Keys) existingKeys(ctx context.Context, ref string) ([]string, bool) {
	if k.Secrets == nil {
		return nil, false
	}
	raw, err := k.Secrets.Get(ctx, ref)
	var sk contract.StorageKeysSecret
	if err != nil || json.Unmarshal(raw, &sk) != nil || sk.Kind != contract.SecretKindStorageKeys {
		return nil, false
	}
	var ids []string
	roles := map[string]bool{}
	for _, e := range sk.Keys {
		ids = append(ids, e.KeyID)
		roles[e.Role] = true
	}
	return ids, roles["write"] && roles["read"]
}

// configMismatch はバケットの設定が期待(バージョニング・Compliance・既定保持 90 日)と食い違う項目。
func configMismatch(cfg contract.BucketConfig) []map[string]any {
	var mm []map[string]any
	if !cfg.Versioning {
		mm = append(mm, map[string]any{"item": "versioning", "expected": true, "actual": cfg.Versioning})
	}
	if cfg.LockMode != wantLockMode {
		var actual any
		if cfg.LockMode != "" {
			actual = cfg.LockMode
		}
		mm = append(mm, map[string]any{"item": "lock_mode", "expected": wantLockMode, "actual": actual})
	}
	if cfg.DefaultRetention != wantDays {
		mm = append(mm, map[string]any{"item": "default_retention_days", "expected": wantDays, "actual": cfg.DefaultRetention})
	}
	return mm
}

// initStorageExisting は発行済みの鍵がある場合: 設定が食い違えば BUCKET_MISMATCH、bucket check が合格すれば何も変えず ALREADY_INITIALIZED(警告)。
// 管理用鍵を受けたときは、作成・発行を飛ばしても発行の後の照合を行う。
func (k *Keys) initStorageExisting(ctx context.Context, ic *isCtx, known []string) (contract.Reply, error) {
	out, err := k.checkBucket(ctx, ic.c, ic.t, ic.t.KeyRef)
	if err != nil {
		return contract.Reply{}, err
	}
	ic.warns = append(ic.warns, mkWarn(contract.CodeAlreadyInitialized, empty{}))
	if ic.admin != nil {
		k.reconcile(ctx, ic, known)
	}
	keys := k.keyRefs(ctx, ic.t.KeyRef)
	return reply(initStorageResult(false, out, keys, nil), append(ic.warns, out.Warnings...)...)
}

// keyRefs は秘密ファイルから keys[] の要素(role・key_ref・key_id)を作る。
func (k *Keys) keyRefs(ctx context.Context, ref string) []keyEl {
	out := []keyEl{}
	if k.Secrets == nil {
		return out
	}
	raw, err := k.Secrets.Get(ctx, ref)
	var sk contract.StorageKeysSecret
	if err != nil || json.Unmarshal(raw, &sk) != nil {
		return out
	}
	for _, role := range issueRoles {
		for _, e := range sk.Keys {
			if e.Role == role {
				out = append(out, keyEl{role, ref, e.KeyID})
			}
		}
	}
	return out
}

func initStorageResult(created bool, out bucketOut, keys []keyEl, revoked []string) map[string]any {
	rv := []map[string]string{}
	for _, id := range revoked {
		rv = append(rv, map[string]string{"key_id": id})
	}
	return map[string]any{"created": created, "lock_mode": out.LockMode, "versioning": out.Versioning, "default_retention_days": out.DefaultDays,
		"keys": keys, "revoked": rv, "permissions": out.Perms}
}

// createBucket は create-bucket を呼ぶ。
func (k *Keys) createBucket(ctx context.Context, ic *isCtx) (res bucketCreated, err error) {
	out, err := provisionCall(ctx, k.Provision, ic.name, map[string]any{"op": "create-bucket", "endpoint": ic.t.Endpoint, "bucket": ic.t.Bucket,
		"versioning": true, "lock_mode": wantLockMode, "default_retention_days": wantDays, "secrets": slotSecrets("admin", ic.admin)})
	if err != nil {
		return res, err
	}
	if json.Unmarshal(out, &res) != nil {
		return res, errInternal()
	}
	return res, nil
}

// bucketCreated は create-bucket の結果(アダプタの応答)。
type bucketCreated struct {
	Created    bool   `json:"created"`
	Versioning bool   `json:"versioning"`
	LockMode   string `json:"lock_mode"`
	Days       int    `json:"default_retention_days"`
}

// initStorageIssue は作成(--reissue でなければ)と発行、鍵の保存、照合、bucket check、(--reissue なら)旧鍵の失効を行う。
func (k *Keys) initStorageIssue(ctx context.Context, ic *isCtx, reissue bool) (contract.Reply, error) {
	var cb bucketCreated
	if !reissue {
		var err error
		if cb, err = k.createBucket(ctx, ic); err != nil {
			return contract.Reply{}, err
		}
		// アダプタの応答も要件(バージョニング・Compliance・既定保持 90 日)と比べる。食い違えば鍵を発行しない。
		if mm := configMismatch(contract.BucketConfig{Versioning: cb.Versioning, LockMode: cb.LockMode, DefaultRetention: cb.Days}); len(mm) > 0 {
			return contract.Reply{}, mkErr(contract.CodeBucketMismatch, map[string]any{"mismatches": mm})
		}
	}
	oldIDs, _ := k.existingKeys(ctx, ic.t.KeyRef)
	issued, orphaned, ierr := k.KeyS.IssueKeys(ctx, contract.IssueRequest{Endpoint: ic.t.Endpoint, Bucket: ic.t.Bucket, Prefix: ic.t.Prefix,
		Roles: issueRoles, MaxRetentionDays: wantDays, Admin: ic.admin})
	defer wipeIssued(issued)
	k.recordOrphans(ctx, ic, orphaned)
	if ierr != nil {
		k.reconcile(ctx, ic, oldIDs)
		return contract.Reply{}, ierr
	}
	newIDs, err := k.saveIssued(ctx, ic, issued)
	if err != nil {
		k.reconcile(ctx, ic, append(oldIDs, newIDs...))
		return contract.Reply{}, err
	}
	k.reconcile(ctx, ic, append(oldIDs, newIDs...))
	out, err := k.checkBucket(ctx, ic.c, ic.t, ic.t.KeyRef)
	if err != nil {
		return contract.Reply{}, err
	}
	var revoked []string
	if reissue {
		if revoked, err = k.revokeOld(ctx, ic, newIDs); err != nil {
			return contract.Reply{}, err
		}
	}
	keys := make([]keyEl, 0, len(issued))
	for _, i := range issued {
		keys = append(keys, keyEl{i.Role, ic.t.KeyRef, i.KeyID})
	}
	return reply(initStorageResult(cb.Created, out, keys, revoked), append(ic.warns, out.Warnings...)...)
}

// saveIssued は発行した鍵を台帳に記録し、秘密ファイルに置く。置けなければ発行した鍵を失効させ、失効できなかった鍵は孤児として台帳に残す。
// 発行した key_id の列は失敗のときも返す(呼び出し側が照合に使う)。台帳を読めない・書けないときは警告にして進む。
func (k *Keys) saveIssued(ctx context.Context, ic *isCtx, issued []contract.IssuedKey) ([]string, error) {
	_, seen, err := k.ledgerKeys(ctx)
	ic.noteLedger(err)
	var ids []string
	for _, i := range issued {
		ids = append(ids, i.KeyID)
		if _, ok := seen[i.KeyID]; ok {
			continue
		}
		ic.noteLedger(k.ledgerPut(ctx, ic.c.RunID, ic.name, ledgerKey{KeyID: i.KeyID, Role: strp(i.Role)}))
	}
	body, err := storageKeysJSON(issued)
	if err != nil {
		return ids, errInternal()
	}
	perr := k.Secrets.Put(ctx, ic.t.KeyRef, body)
	wipeBytes(body)
	if perr != nil {
		rev, nf, rerr := k.KeyS.RevokeKey(ctx, contract.RevokeRequest{Endpoint: ic.t.Endpoint, Bucket: ic.t.Bucket, KeyIDs: ids, Admin: ic.admin})
		var left []string
		for _, id := range ids {
			if rerr != nil || (!contains(rev, id) && !contains(nf, id)) {
				left = append(left, id)
			}
		}
		k.recordOrphans(ctx, ic, left)
		if rerr == nil {
			k.markRevoked(ctx, ic, rev, false)
		}
		return ids, errInternal()
	}
	return ids, nil
}

// reconcile は発行の後の照合: check(枠 admin)が返す製品の印の付いた鍵のうち、台帳にも known にも無いものを孤児として記録して失効させる。
// check 自体が失敗したら警告 KEY_RECONCILE_UNVERIFIED(発行の結果は有効のまま)。管理用鍵が無ければ何もしない。
func (k *Keys) reconcile(ctx context.Context, ic *isCtx, known []string) {
	if ic.admin == nil {
		return
	}
	ids, err := checkAdmin(ctx, k.Provision, ic.name, ic.t.Endpoint, ic.t.Bucket, ic.t.Prefix, ic.admin)
	if err != nil {
		ic.warns = append(ic.warns, mkWarn(contract.CodeKeyReconcileUnverified, map[string]string{"reason": adapterCode(err)}))
		return
	}
	_, led, lerr := k.ledgerKeys(ctx)
	if lerr != nil {
		ic.noteLedger(lerr)
		return
	}
	var orphans []string
	for _, id := range ids {
		if _, ok := led[id]; ok || contains(known, id) {
			continue
		}
		orphans = append(orphans, id)
		ic.noteLedger(k.ledgerPut(ctx, ic.c.RunID, ic.name, ledgerKey{KeyID: id, Orphaned: true}))
	}
	if len(orphans) == 0 {
		return
	}
	rev, _, rerr := k.KeyS.RevokeKey(ctx, contract.RevokeRequest{Endpoint: ic.t.Endpoint, Bucket: ic.t.Bucket, KeyIDs: orphans, Admin: ic.admin})
	if rerr != nil {
		return // 失効できなければ orphaned のまま残す(init --adopt の旧鍵の失効で扱う)
	}
	k.markRevoked(ctx, ic, rev, true)
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// revokeOld は台帳にあって失効していない旧鍵(今回発行した鍵を除く)を revoke-keys に渡す。失効した key_id の列を返す。
func (k *Keys) revokeOld(ctx context.Context, ic *isCtx, newIDs []string) ([]string, error) {
	order, led, err := k.ledgerKeys(ctx)
	if err != nil {
		return nil, err
	}
	var old []string
	for _, id := range order {
		if led[id].RevokedAt == nil && !contains(newIDs, id) {
			old = append(old, id)
		}
	}
	if len(old) == 0 {
		return nil, nil
	}
	rev, _, err := k.KeyS.RevokeKey(ctx, contract.RevokeRequest{Endpoint: ic.t.Endpoint, Bucket: ic.t.Bucket, KeyIDs: old, Admin: ic.admin})
	if err != nil {
		return nil, err
	}
	k.markRevoked(ctx, ic, rev, false)
	return rev, nil
}
