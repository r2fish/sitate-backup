package restore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const destKind = "restore-dest"

// destCall は復元先アダプタを 1 回呼ぶ(要求に contract を足す)。
func destCall(ctx context.Context, ar contract.AdapterRunner, runID, name string, req map[string]any) (json.RawMessage, error) {
	req["contract"] = "sitate.restore-dest/v1"
	b, err := json.Marshal(req)
	if err != nil {
		return nil, internalErr("")
	}
	return ar.Run(ctx, contract.RunContext{RunID: runID}, destKind, name, b, nil)
}

// pairOf はアダプタの組(create_destroy / start_stop)。読めなければ start_stop(ssh-host)。
func pairOf(ctx context.Context, ar contract.AdapterRunner, name string) string {
	d, err := ar.Declaration(ctx, destKind, name)
	if err == nil {
		var x struct {
			Pair string `json:"pair"`
		}
		if json.Unmarshal(d.Raw, &x) == nil && x.Pair == "create_destroy" {
			return x.Pair
		}
	}
	return "start_stop"
}

func teardownOp(pair string) string {
	if pair == "create_destroy" {
		return "destroy"
	}
	return "stop"
}

// sshSecretKey は枠 restore-dest-ssh に入れる核の復元先用の秘密鍵(参照名 sitate-restore)。取れなければ空。
func sshSecretKey(ctx context.Context, sec contract.Secrets) string {
	if sec == nil {
		return ""
	}
	raw, err := sec.Get(ctx, contract.RestoreKeyRef)
	if err != nil {
		return ""
	}
	var k contract.SSHKeySecret
	if json.Unmarshal(raw, &k) != nil {
		return ""
	}
	return k.PrivateKey
}

// teardownResult は stop / destroy の結果と、続く inspect(session_id)の結果。
type teardownResult struct {
	Done       bool
	VerifiedBy string
	Remaining  []resRef
	Absent     bool // inspect が全資源 absent を返した
}

// teardownSession は台帳の記録の所在で stop / destroy と inspect を呼ぶ(spec 3.2.4 の後片付けの要求の所在)。
// 資源が無いと確かめられたときだけ Absent。呼び出しの失敗は残存として扱う。
func teardownSession(ctx context.Context, ar contract.AdapterRunner, sec contract.Secrets, rec ledgerRec) teardownResult {
	tr := teardownResult{VerifiedBy: "ssh", Remaining: rec.Resources}
	fp := any(nil)
	if rec.HostKeyFP != nil {
		fp = *rec.HostKeyFP
	}
	secrets := map[string]string{}
	pair := pairOf(ctx, ar, rec.Adapter)
	if pair == "start_stop" {
		secrets["restore-dest-ssh"] = sshSecretKey(ctx, sec)
	}
	op := teardownOp(pair)
	raw, err := destCall(ctx, ar, rec.RunID, rec.Adapter, map[string]any{"op": op, "session_id": rec.SessionID, "dest": rec.Dest, "host_key_fp": fp, "resources": nonNilRes(rec.Resources), "secrets": secrets})
	if err == nil {
		var r struct {
			Done       bool     `json:"done"`
			VerifiedBy string   `json:"verified_by"`
			Remaining  []resRef `json:"remaining"`
		}
		if json.Unmarshal(raw, &r) == nil {
			tr.Done, tr.Remaining = r.Done, nonNilRes(r.Remaining)
			if r.VerifiedBy != "" {
				tr.VerifiedBy = r.VerifiedBy
			}
		}
	}
	raw, err = destCall(ctx, ar, rec.RunID, rec.Adapter, map[string]any{"op": "inspect", "session_id": rec.SessionID, "dest": rec.Dest, "host_key_fp": fp, "resources": nonNilRes(rec.Resources), "secrets": secrets})
	if err == nil {
		var r struct {
			State     string `json:"state"`
			Resources []struct {
				Type, ID string
			} `json:"resources"`
		}
		if json.Unmarshal(raw, &r) == nil && r.State == "absent" && len(r.Resources) == 0 {
			tr.Absent, tr.Done, tr.Remaining = true, true, []resRef{}
		}
	}
	return tr
}

func nonNilRes(r []resRef) []resRef {
	if r == nil {
		return []resRef{}
	}
	return r
}

// errResources はアダプタの失敗が運んだ resources[](作りかけの資源。Error.Adapter)。
func errResources(err error) []resRef {
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Adapter == nil {
		return nil
	}
	out := make([]resRef, 0, len(ce.Adapter.Resources))
	for _, r := range ce.Adapter.Resources {
		out = append(out, resRef{Type: r.Type, ID: r.ID})
	}
	return out
}

// mapDestErr は復元先アダプタの error.code を原因コードへ写す(spec 3.1.3 の写像の表)。
// ADAPTER_ERROR 以外、写像の無い code はそのまま返す。expectedFP は固定した指紋(hostkey_mismatch の expected_fp)。
func mapDestErr(op string, err error, expectedFP string) error {
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeAdapterError || ce.Adapter == nil {
		return err
	}
	a := ce.Adapter
	var d struct {
		Reason     string   `json:"reason"`
		NeededGB   any      `json:"needed_gb"`
		FreeGB     any      `json:"free_gb"`
		Mismatches []string `json:"mismatches"`
		Prereq     []any    `json:"prereq"`
	}
	_ = json.Unmarshal(a.Detail, &d)
	switch a.Code {
	case "hostkey_mismatch":
		return newErr(contract.CodeRestoreHostkeyMismatch, map[string]any{"expected_fp": expectedFP, "seen_fp": a.SeenFP, "session_id": nil})
	case "auth":
		if op == "enroll" {
			return newErr(contract.CodeRestoreEnrollMismatch, enrollMismatchDetail("auth", "", ""))
		}
		return newErr(contract.CodeRestoreHostDenied, map[string]any{"reason": "auth"})
	case "quota":
		return newErr(contract.CodeRestoreHostDenied, map[string]any{"reason": "quota"})
	case "capacity":
		return newErr(contract.CodeStorageUnreachable, map[string]any{"target": "restore_host", "reason": "capacity", "http_status": nil})
	case "unreachable":
		reason := "connect"
		if d.Reason == "timeout" {
			reason = "timeout"
		}
		return newErr(contract.CodeStorageUnreachable, map[string]any{"target": "restore_host", "reason": reason, "http_status": nil})
	case "disk_short", "enroll_mismatch", "prereq_missing", "enroll_failed":
		m := enrollMismatchDetail(a.Code, "", "")
		if a.Code == "disk_short" {
			m["needed_gb"], m["free_gb"] = d.NeededGB, d.FreeGB
		}
		if d.Mismatches != nil {
			m["mismatches"] = d.Mismatches
		}
		if d.Prereq != nil {
			m["prereq"] = d.Prereq
		}
		return newErr(contract.CodeRestoreEnrollMismatch, m)
	}
	return err
}

func toResources(rs []resRef) []contract.Resource {
	out := make([]contract.Resource, 0, len(rs))
	for _, r := range rs {
		out = append(out, contract.Resource{Kind: r.Type, ID: r.ID})
	}
	return out
}
