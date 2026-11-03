package cli

import (
	"context"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// needsInit は記録を持つホスト(initialized が真)を前提にするサブコマンド。持たなければ NOT_INITIALIZED(終了 3)。
// restore と list は、記録を持たない新しいホストでは --endpoint などを受ける(spec 3.1.2.3・3.1.2.4)ので別に扱う。
var needsInit = map[string]bool{
	"backup": true, "verify": true, "extend": true, "prune": true, "hold set": true, "restore-test": true, "apply": true,
	"propose": true, "approve": true, "cancel": true, "attest": true, "kit": true, "kit confirm": true,
	"restore-dest check": true, "restore-dest teardown": true, "restore-dest release": true, "source check": true,
	"bucket check": true, "upgrade": true,
}

// required はサブコマンドごとの必須の引数(spec 3.1.2 の「主な引数」。記録を持たない新しいホストの restore・list は別)。
var required = map[string][]string{
	"init-storage":        {"endpoint", "bucket", "prefix", "adapter"},
	"source enroll":       {"host"},
	"source survey":       {"host"},
	"restore-dest enroll": {"dest", "adapter"},
	"notify set":          {"to", "secret-ref"},
	"def validate":        {"file"},
	"phrase verify":       {"phrase-stdin"},
}

// newHostRequired は記録を持たない新しいホストの restore・list が必須とする引数(spec 3.1.2.3・3.1.2.4)。
var newHostRequired = map[string][]string{
	"restore": {"endpoint", "bucket", "prefix", "keyring", "key-ref", "repo-id"},
	"list":    {"endpoint", "bucket", "prefix", "keyring", "key-ref", "phrase-stdin"},
}

// precheck は部品を呼ぶ前の前提の確認: 必須の引数(ARG_INVALID の missing)と、記録を持つホストの前提(NOT_INITIALIZED)。
func precheck(ctx context.Context, d *Deps, p *parsed) *contract.Error {
	key := strings.Join(p.sub.Words, " ")
	if key == "attest" && unsignedAttestStep(p) {
		return nil // 署名を要さない 3 段は init の前も記録できる(spec 3.1.2 の「attest の段」。bootstrap の段 (8) は init の前に呼ぶ)
	}
	if needsInit[key] {
		return checkInitialized(ctx, d)
	}
	if names, ok := newHostRequired[key]; ok {
		if e := checkInitialized(ctx, d); e == nil {
			return nil
		}
		if _, viaSnapshotKey := p.args["snapshot-key-stdin"]; viaSnapshotKey {
			return nil // 復元先の restore は状態を持たず、所在と鍵を標準入力と引数で受ける(spec 3.5)
		}
		return missing(p, names[:1])
	}
	if key == "init" {
		if _, adopt := p.args["adopt"]; !adopt {
			return missing(p, []string{"approval-pubkey"})
		}
		return nil
	}
	return missing(p, required[key])
}

// unsignedAttestStep は --step が署名を要さない段(admin_access_closed・admin_access_opened・dest_disposed)か。
func unsignedAttestStep(p *parsed) bool {
	v := p.args["step"]
	if len(v) != 1 {
		return false
	}
	switch v[0] {
	case "admin_access_closed", "admin_access_opened", "dest_disposed":
		return true
	}
	return false
}

func missing(p *parsed, names []string) *contract.Error {
	for _, n := range names {
		if _, ok := p.args[n]; !ok {
			return argErr("--"+n, "missing")
		}
	}
	return nil
}

func checkInitialized(ctx context.Context, d *Deps) *contract.Error {
	v, err := d.State.Get(ctx, "initialized")
	if err != nil {
		return toError(err)
	}
	if string(v) != "true" {
		return newErr(contract.CodeNotInitialized, nil)
	}
	return nil
}
