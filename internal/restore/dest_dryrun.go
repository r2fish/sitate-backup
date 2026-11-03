package restore

// restore-dest enroll --dry-run(spec 3.1.1 の dry-run の行、3.2.4 の enroll。D-0165)。導入の手順 3 は定義を組み立てる前に呼ぶので、
// 定義(候補)を読まず、引数(--dest・--adapter・--admin-ref・--host-key-fp)から動く。アダプタの enroll の dry_run だけを呼び、
// 復元先へは接続してホスト鍵を返し初期資格情報の受理と前提を読むだけ。復元先の鍵を作らず、固定値も登録の記録も秘密ファイルも置かない。
// 拒否と前提の欠けは plan.errors[] に写す。

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type dryPlanErr struct {
	Code   contract.Code  `json:"code"`
	Detail map[string]any `json:"detail"`
}

func (r *Restore) enrollDryRun(ctx context.Context, c contract.Call) (contract.Reply, error) {
	at := arg(c, "dest")
	if _, _, ok := parseDestAt(at); !ok {
		return contract.Reply{}, argInvalid("--dest", "format")
	}
	adapter := arg(c, "adapter")
	if adapter == "" {
		adapter = "ssh-host"
	}
	secrets := map[string]string{}
	if ref := arg(c, contract.ArgAdminRef); ref != "" {
		var s string
		if v, ok := c.Secrets[ref]; ok && json.Unmarshal(v, &s) == nil {
			secrets["dest_key"] = s
		}
	}
	req := map[string]any{"op": "enroll", "dry_run": true, "dest": destRef{At: at, KeyRef: "dest-key"}, "secrets": secrets}
	want := arg(c, "host-key-fp")
	if want != "" {
		req["host_key_fp"] = want
	}
	errs := []dryPlanErr{}
	var fp any
	raw, err := destCall(ctx, r.Adapters, c.RunID, adapter, req)
	if err != nil {
		var ce *contract.Error
		if ctx.Err() != nil || !errors.As(mapDestErr("enroll", err, want), &ce) {
			return contract.Reply{}, err
		}
		errs = append(errs, toDryPlanErr(ce))
	} else {
		var res struct {
			FP     string `json:"host_key_fp"`
			Prereq []struct {
				Name string `json:"name"`
				OK   bool   `json:"ok"`
			} `json:"prereq"`
		}
		if json.Unmarshal(raw, &res) != nil || res.FP == "" {
			return contract.Reply{}, internalErr("output")
		}
		fp = res.FP
		var short []any
		for _, p := range res.Prereq {
			if !p.OK {
				short = append(short, map[string]any{"name": p.Name, "ok": false})
			}
		}
		if len(short) > 0 {
			m := enrollMismatchDetail("prereq_missing", "", "")
			m["prereq"] = short
			errs = append(errs, dryPlanErr{contract.CodeRestoreEnrollMismatch, m})
		}
	}
	plan := map[string]any{"reversible": true, "errors": errs, "host_key_fp": fp}
	return reply(map[string]any{"host_key_fp": fp, "isolation": []isoItem{}, "replaced": false, "plan": plan})
}

func toDryPlanErr(ce *contract.Error) dryPlanErr {
	d := map[string]any{}
	_ = json.Unmarshal(ce.Detail, &d)
	return dryPlanErr{ce.Code, d}
}
