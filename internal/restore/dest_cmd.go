package restore

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// probeDest は復元先へ固定した指紋で接続して隔離の検査を流す(spec 3.5 の表の 5)。
func (r *Restore) probeDest(ctx context.Context, d *defDoc, fp string) (contract.DestProbeOut, error) {
	var out contract.DestProbeOut
	host, port, ok := parseDestAt(d.Restore.Dest.At)
	if !ok {
		return out, argInvalid("--dest", "format")
	}
	_, err := r.runScript(ctx, r.destConn(ctx, host, port, restoreUser, fp), probeScript(d.forbiddenNames(), port), &out)
	return out, err
}

// RestoreDestEnroll は復元先の登録(spec 3.1.2、3.5「復元先の登録」)。アダプタの enroll の後、専用ユーザーで隔離の検査を行い、
// 合格したときだけ固定値(host_keys[] の role: restore_dest と登録の記録)を書く(--dry-run と検査の不合格では書かない)。
func (r *Restore) RestoreDestEnroll(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if r == nil || r.State == nil || r.Adapters == nil {
		return contract.Reply{}, internalErr("")
	}
	// seal の後の再登録は危険(復元先の変更。restore-change)で、承認の後の apply の中で行う(spec 3.1.2)。seal の判定は state.json の sealed を直接読む。
	var sealed bool
	if _, err := item(ctx, r.State, "sealed", &sealed); err != nil {
		return contract.Reply{}, err
	}
	if sealed {
		e := newErr(contract.CodeApprovalRequired, map[string]any{"kind": "restore-change"})
		e.Next = []contract.Next{{Argv: []string{"sitate", "propose", "restore-change"}, RequiresApproval: true}}
		return contract.Reply{}, e
	}
	d, _, err := r.readDef(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	host, _, _ := parseDestAt(d.Restore.Dest.At)
	if c.DryRun {
		return reply(map[string]any{"host_key_fp": nil, "isolation": []isoItem{}, "replaced": false})
	}
	var admin *contract.Secret
	if ref := arg(c, contract.ArgAdminRef); ref != "" {
		if v, ok := c.Secrets[ref]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil {
				admin = contract.NewSecret([]byte(s))
				defer admin.Wipe()
			}
		}
	}
	prev, err := pinnedFP(ctx, r.State, host)
	if err != nil {
		return contract.Reply{}, err
	}
	// 照らす指紋: 人が確かめた新しい指紋(--host-key-fp。seal の前)があれば固定値の代わりにそれ、無ければ今の固定値、どちらも無ければ初回の登録(省く)。
	want := prev
	if v := arg(c, "host-key-fp"); v != "" {
		want = v
	}
	out, err := enrollCore(ctx, r.Adapters, r.State, r.Secrets, c.RunID, d, admin, want)
	if err != nil {
		return contract.Reply{}, err
	}
	// 固定値を書いてから検査する(spec 3.5 の「復元先の登録」)。検査の不合格は RESTORE_ISOLATION_FAILED で、固定値は残るので
	// next の restore-dest check が同じ復元先を検査し直せる。
	if err := writeEnrollState(ctx, r.State, host, out.FP, enrollRecord(out)); err != nil {
		return contract.Reply{}, err
	}
	po, err := r.probeDest(ctx, d, out.FP)
	if err != nil {
		return contract.Reply{}, err
	}
	items, failed := isolationItems(po)
	if len(failed) > 0 {
		return contract.Reply{}, newErr(contract.CodeRestoreIsolationFailed, map[string]any{"failed": failed})
	}
	return reply(map[string]any{"host_key_fp": out.FP, "isolation": items, "replaced": prev != "" && prev != out.FP})
}

// RestoreDestCheck は復元先の到達性と隔離の再検査(読み取りのみ)。固定した指紋で接続する。--session は復元セッションとともにイテレーション 4 以降。
func (r *Restore) RestoreDestCheck(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if r == nil || r.State == nil {
		return contract.Reply{}, internalErr("")
	}
	if hasArg(c, "session") {
		return contract.Reply{}, contract.ErrNotImplemented("restore.Restore.RestoreDestCheck.session")
	}
	d, _, err := r.readDef(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	host, _, _ := parseDestAt(d.Restore.Dest.At)
	fp, err := pinnedFP(ctx, r.State, host)
	if err != nil {
		return contract.Reply{}, err
	}
	if fp == "" {
		return contract.Reply{}, newErr(contract.CodeRestoreEnrollMismatch, enrollMismatchDetail("enroll_mismatch", "", ""))
	}
	po, err := r.probeDest(ctx, d, fp)
	if err != nil {
		return contract.Reply{}, err
	}
	items, _ := isolationItems(po)
	return reply(map[string]any{"isolation": items})
}

// enrollMismatchDetail は RESTORE_ENROLL_MISMATCH の detail(os・arch は unsupported_platform のときだけ値を持つ)。
func enrollMismatchDetail(reason, osName, arch string) map[string]any {
	var o, a any
	if reason == "unsupported_platform" {
		o, a = osName, arch
	}
	return map[string]any{"reason": reason, "mismatches": []string{}, "prereq": []any{}, "needed_gb": nil, "free_gb": nil, "os": o, "arch": a}
}
