package keys

// bucket check(spec 3.1.2、3.2.6「操作の正規化名」、4.2「bucket check の試し方」): 禁止すべき操作が拒否されるか。
// 操作ごとに、試す(StoreProbe.Try)か鍵の権限(アダプタの check を枠 key で呼んで読んだ capabilities)で判定するかは表のとおり。設定を変える操作は試さない。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const (
	permTry = "try"
	permCap = "cap"
)

// permSide はある役割の鍵についての期待(""は要素を作らない)と判定のしかた。
type permSide struct{ exp, mode string }

type opRow struct {
	op   string
	w, r permSide
}

// opTable は 3.2.6 の操作の表(順序は表のとおり)。
var opTable = []opRow{
	{"put", permSide{"allow", permTry}, permSide{"deny", permCap}},
	{"list_versions", permSide{"allow", permTry}, permSide{"allow", permTry}},
	{"get_version", permSide{"allow", permTry}, permSide{"allow", permTry}},
	{"get_retention", permSide{"allow", permTry}, permSide{}},
	{"get_bucket_config", permSide{"allow", permTry}, permSide{}},
	{"extend_retention", permSide{"allow", permCap}, permSide{"deny", permCap}},
	{"delete_version", permSide{"allow", permCap}, permSide{"deny", permCap}},
	{"retention_shorten", permSide{"deny", permTry}, permSide{"deny", permTry}},
	{"unversioned_delete", permSide{"deny", permTry}, permSide{"deny", permTry}},
	{"bucket_policy", permSide{"deny", permCap}, permSide{"deny", permCap}},
	{"lifecycle", permSide{"deny", permCap}, permSide{"deny", permCap}},
	{"default_retention", permSide{"deny", permCap}, permSide{"deny", permCap}},
	{"governance_bypass", permSide{"deny", permCap}, permSide{"deny", permCap}},
	{"legal_hold", permSide{"deny", permTry}, permSide{"deny", permTry}},
	{"bucket_delete", permSide{"deny", permCap}, permSide{"deny", permCap}},
	{"key_manage", permSide{"deny", permCap}, permSide{"deny", permCap}},
}

type permItem struct {
	Op      string `json:"op"`
	KeyRole string `json:"key_role"`
	Expect  string `json:"expected"`
	Actual  string `json:"actual"`
}

// target は保管先の所在(endpoint・bucket・prefix・鍵の参照名)。
type target struct{ Endpoint, Bucket, Prefix, KeyRef string }

// storageTarget は引数(あれば)と定義の storage[0] から保管先の所在を読む。seal の前は候補、後は承認済みの定義。
func (k *Keys) storageTarget(ctx context.Context, c contract.Call) (t target, defFound bool, err error) {
	t.Endpoint, _ = argOne(c, contract.ArgEndpoint)
	t.Bucket, _ = argOne(c, contract.ArgBucket)
	t.Prefix, _ = argOne(c, contract.ArgPrefix)
	var sealed bool
	if _, err = k.getItem(ctx, "sealed", &sealed); err != nil {
		return t, false, err
	}
	var def []byte
	if sealed {
		def, _, err = k.State.ReadDef(ctx)
		defFound = len(def) > 0
	} else {
		def, _, defFound, err = k.State.ReadCandidate(ctx)
	}
	if err != nil {
		return t, false, err
	}
	var d struct {
		Storage []struct {
			Endpoint string `json:"endpoint"`
			Bucket   string `json:"bucket"`
			Prefix   string `json:"prefix"`
			KeyRef   string `json:"key_ref"`
		} `json:"storage"`
	}
	if defFound && json.Unmarshal(def, &d) == nil && len(d.Storage) > 0 {
		s := d.Storage[0]
		t.KeyRef = s.KeyRef
		if t.Endpoint == "" {
			t.Endpoint = s.Endpoint
		}
		if t.Bucket == "" {
			t.Bucket = s.Bucket
		}
		if _, ok := argOne(c, contract.ArgPrefix); !ok {
			t.Prefix = s.Prefix
		}
	}
	return t, defFound, nil
}

// roleKeys は参照名の storage_keys の秘密から役割ごとの鍵(JSON `{"key_id","secret"}` の秘密)を読む。呼び出し側が Wipe する。
// Secrets が注入されていない(試験の偽の環境)ときは鍵の材料なしで進む。参照名が無い・役割が欠けるときは SECRET_MISSING。
func (k *Keys) roleKeys(ctx context.Context, ref string) (map[string]*contract.Secret, error) {
	out := map[string]*contract.Secret{}
	if k.Secrets == nil {
		return nil, errInternal()
	}
	raw, err := k.Secrets.Get(ctx, ref)
	if err != nil {
		return nil, mkErr(contract.CodeSecretMissing, map[string]string{"ref": ref})
	}
	var sk contract.StorageKeysSecret
	if json.Unmarshal(raw, &sk) != nil || sk.Kind != contract.SecretKindStorageKeys {
		return nil, mkErr(contract.CodeSecretMissing, map[string]string{"ref": ref})
	}
	for _, e := range sk.Keys {
		b, _ := json.Marshal(map[string]string{"key_id": e.KeyID, "secret": e.Secret})
		out[e.Role] = contract.NewSecret(b)
	}
	for _, role := range []string{"write", "read"} {
		if out[role] == nil {
			wipeAll(out)
			return nil, mkErr(contract.CodeSecretMissing, map[string]string{"ref": ref})
		}
	}
	return out, nil
}

func wipeAll(m map[string]*contract.Secret) {
	for _, s := range m {
		if s != nil {
			s.Wipe()
		}
	}
}

func (k *Keys) provisionName(c contract.Call) string {
	if n, ok := argOne(c, "adapter"); ok && n != "" {
		return n
	}
	if k.ProvisionName != "" {
		return k.ProvisionName
	}
	return "b2"
}

// bucketOut は bucket check の判定の結果。
type bucketOut struct {
	LockMode    string
	Versioning  bool
	DefaultDays int
	Perms       []permItem
	Warnings    []contract.Warning
}

// BucketCheck は契約 contract.Keys を満たす。
func (k *Keys) BucketCheck(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := errNeedDeps(k.State != nil, k.Store != nil, k.Probe != nil, k.Provision != nil, k.Recorder != nil); err != nil {
		return contract.Reply{}, err
	}
	t, found, err := k.storageTarget(ctx, c)
	if err != nil {
		return contract.Reply{}, err
	}
	ref, _ := argOne(c, "key-ref")
	if ref == "" { // 省くと定義の storage[0].key_ref。seal の前は候補の値(spec 3.1.2 の bucket check。D-0160)。候補が無ければ DEF_INVALID
		if !found || t.KeyRef == "" {
			return contract.Reply{}, mkErr(contract.CodeDefInvalid, map[string]any{"issues": []map[string]string{{"pointer": "/storage/0/key_ref", "reason": "missing"}}})
		}
		ref = t.KeyRef
	}
	out, err := k.checkBucket(ctx, c, t, ref)
	if err != nil {
		return contract.Reply{}, err
	}
	var sealed bool // last_bucket_check は seal の後の記録(spec 4.5)
	if _, err := k.getItem(ctx, "sealed", &sealed); err != nil {
		return contract.Reply{}, err
	}
	if sealed {
		if err := k.setItems(ctx, "last_bucket_check", map[string]any{"storage_id": storageID(t), "permissions": out.Perms}); err != nil {
			return contract.Reply{}, err
		}
	}
	return reply(map[string]any{"lock_mode": out.LockMode, "versioning": out.Versioning, "permissions": out.Perms}, out.Warnings...)
}

// storageID は保管先の識別子(endpoint・bucket・prefix の RFC 8785 直列化の SHA-256。spec 4.5)。
func storageID(t target) string {
	b, _ := json.Marshal(map[string]string{"endpoint": t.Endpoint, "bucket": t.Bucket, "prefix": t.Prefix})
	h, err := contract.CanonicalHash(b)
	if err != nil {
		return ""
	}
	return h
}

// writeProbe は probe/ に試験用オブジェクトを書く(書く前に書き込み予定を実行の記録に残し、書いた版を index/probe に残す)。
func (k *Keys) writeProbe(ctx context.Context, t target) error {
	rnd := make([]byte, 16)
	if _, err := rand.Read(rnd); err != nil {
		return errInternal()
	}
	key := "probe/" + hex.EncodeToString(rnd)
	if err := k.recordIntent(ctx, key); err != nil {
		return err
	}
	vid, err := k.Probe.PutProbe(ctx, key, rnd)
	if err != nil {
		return err
	}
	var rows []map[string]any
	if _, err := k.getItem(ctx, "index/probe", &rows); err != nil {
		return err
	}
	now, err := k.now(ctx)
	if err != nil {
		return err
	}
	rows = append(rows, map[string]any{"storage_id": storageID(t), "key": key, "version_id": vid, "size": len(rnd), "written_at": fmtTime(now), "retention_long": false})
	return k.setItems(ctx, "index/probe", rows)
}

// recordIntent は保管先へキーを書く前に書き込み予定(write_intent)を実行の記録に残す(spec 4.2、4.5)。
func (k *Keys) recordIntent(ctx context.Context, keys ...string) error {
	b, err := json.Marshal(map[string]any{"kind": "write_intent", "body": map[string]any{"keys": keys}})
	if err != nil {
		return errInternal()
	}
	_, err = k.Recorder.Record(ctx, b)
	return err
}

// checkBucket は writeProbe と全操作の判定を行い、禁止すべき操作が通れば KEY_TOO_POWERFUL、許すべき操作が通らなければ KEY_MISSING_PERMISSION を返す。
func (k *Keys) checkBucket(ctx context.Context, c contract.Call, t target, ref string) (bucketOut, error) {
	var out bucketOut
	keys, err := k.roleKeys(ctx, ref)
	if err != nil {
		return out, err
	}
	defer wipeAll(keys)
	cfg, err := k.Store.BucketConfig(ctx)
	if err != nil {
		return out, err
	}
	if mm := configMismatch(cfg); len(mm) > 0 {
		return out, mkErr(contract.CodeBucketMismatch, map[string]any{"mismatches": mm})
	}
	out.LockMode, out.Versioning, out.DefaultDays = cfg.LockMode, cfg.Versioning, cfg.DefaultRetention
	if out.LockMode == "" {
		out.LockMode = "NONE"
	}
	if err := k.writeProbe(ctx, t); err != nil {
		return out, err
	}
	name := k.provisionName(c)
	frames := map[string]capsFrame{}
	for _, role := range []string{"write", "read"} {
		f, err := checkKey(ctx, k.Provision, name, t.Endpoint, t.Bucket, t.Prefix, role, keys[role])
		if err != nil {
			return out, err
		}
		frames[role] = f
	}
	over, missing, err := k.evalPerms(ctx, &out, name, frames)
	if err != nil {
		return out, err
	}
	for _, role := range []string{"write", "read"} {
		if len(over[role]) > 0 {
			return out, mkErr(contract.CodeKeyTooPowerful, map[string]any{"role": role, "ops": over[role]})
		}
	}
	for _, role := range []string{"write", "read"} {
		if len(missing[role]) > 0 {
			return out, mkErr(contract.CodeKeyMissingPermission, map[string]any{"role": role, "reason": "denied", "ops": missing[role]})
		}
	}
	return out, nil
}

// softWarning は不合格にせず警告で返す「通った」操作(版指定なしの削除は書き込み鍵だけ、リーガルホールドは両方)。
func softWarning(op, role, provider string) (contract.Warning, bool) {
	switch {
	case op == "unversioned_delete" && role == "write":
		return mkWarn(contract.CodeKeyAllowsUnversionedDelete, map[string]string{"role": "write"}), true
	case op == "legal_hold":
		return mkWarn(contract.CodeKeyLegalHoldAllowed, map[string]string{"role": role, "provider": provider}), true
	}
	return contract.Warning{}, false
}

// judge は 1 つの操作の実際を allow / deny / untested で返す。
func (k *Keys) judge(ctx context.Context, role, op, mode string, f capsFrame) (string, error) {
	if mode == permTry {
		allowed, err := k.Probe.Try(ctx, role, op)
		if err != nil {
			return "", err
		}
		if allowed {
			return "allow", nil
		}
		return "deny", nil
	}
	if f.Null {
		return "untested", nil
	}
	if slices.Contains(f.Caps, op) {
		return "allow", nil
	}
	return "deny", nil
}

// evalPerms は表の全部の操作の実際を判定して out に積み、禁止すべきなのに通った操作(over)と許すべきなのに通らない操作(missing)を役割ごとに返す。
func (k *Keys) evalPerms(ctx context.Context, out *bucketOut, name string, frames map[string]capsFrame) (over, missing map[string][]string, err error) {
	over, missing = map[string][]string{}, map[string][]string{}
	for _, row := range opTable {
		for _, role := range []string{"write", "read"} {
			side := row.w
			if role == "read" {
				side = row.r
			}
			if side.exp == "" {
				continue
			}
			actual, err := k.judge(ctx, role, row.op, side.mode, frames[role])
			if err != nil {
				return nil, nil, err
			}
			out.Perms = append(out.Perms, permItem{Op: row.op, KeyRole: role, Expect: side.exp, Actual: actual})
			switch {
			case side.exp == "deny" && actual == "allow":
				if w, soft := softWarning(row.op, role, name); soft {
					out.Warnings = append(out.Warnings, w)
				} else {
					over[role] = append(over[role], row.op)
				}
			case side.exp == "allow" && actual == "deny":
				missing[role] = append(missing[role], row.op)
			}
		}
	}
	return over, missing, nil
}
