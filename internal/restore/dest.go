package restore

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const restoreUser = "sitate-restore"

// hostKeyRec は state.json の host_keys[] の要素。
type hostKeyRec struct {
	Role      string  `json:"role"`
	Host      string  `json:"host"`
	HostKeyFP string  `json:"host_key_fp"`
	SessionID *string `json:"session_id"`
}

// pinnedFP は host_keys[] の role: restore_dest の host の固定した指紋(無ければ空)。
func pinnedFP(ctx context.Context, st contract.StateFiles, host string) (string, error) {
	var hks []hostKeyRec
	if _, err := item(ctx, st, "host_keys", &hks); err != nil {
		return "", err
	}
	for _, h := range hks {
		if h.Role == "restore_dest" && h.Host == host {
			return h.HostKeyFP, nil
		}
	}
	return "", nil
}

// enrollOut は 復元先アダプタの enroll の結果。
type enrollOut struct {
	FP     string
	Egress []string
}

// enrollCore は復元先アダプタの enroll を呼ぶ(spec 3.2.4)。wantFP が空でなければ、返った指紋が一致することを確かめる(違えば RESTORE_HOSTKEY_MISMATCH)。
// 状態は書かない。admin は初期資格情報(OpenSSH 形式の秘密鍵の文字列)。
func enrollCore(ctx context.Context, ar contract.AdapterRunner, st contract.StateFiles, sec contract.Secrets, runID string, d *defDoc, admin *contract.Secret, wantFP string) (enrollOut, error) {
	if d.Restore.SSHFrom == "" { // 受信を許す送信元を決められないので呼ばない(spec 3.2.4)
		return enrollOut{}, defMissing("/restore/ssh_from")
	}
	key, err := restoreKey(ctx, sec, true)
	if err != nil {
		return enrollOut{}, err
	}
	egress, err := resolveEgress(ctx, d)
	if err != nil {
		return enrollOut{}, err
	}
	allow := []string{}
	if d.Restore.SSHFrom != "" {
		allow = append(allow, d.Restore.SSHFrom)
	}
	secrets := map[string]string{}
	if admin != nil {
		secrets["dest_key"] = string(admin.Reveal())
	}
	raw, err := destCall(ctx, ar, runID, d.Restore.Adapter, map[string]any{"op": "enroll", "dest": d.Restore.Dest, "authorized_key": key.PublicKey,
		"allow_ssh_from": allow, "egress_allow": egress, "egress_host": d.endpointHost(), "secrets": secrets})
	if err != nil {
		return enrollOut{}, mapDestErr("enroll", err, "")
	}
	var res struct {
		FP string `json:"host_key_fp"`
	}
	if json.Unmarshal(raw, &res) != nil || res.FP == "" {
		return enrollOut{}, internalErr("output")
	}
	if wantFP != "" && wantFP != res.FP {
		return enrollOut{}, newErr(contract.CodeRestoreHostkeyMismatch, map[string]any{"expected_fp": wantFP, "seen_fp": res.FP, "session_id": nil})
	}
	return enrollOut{FP: res.FP, Egress: egress}, nil
}

// enrollRecord は restore_dest_enrollment の値(送信を許す IP アドレスの固定値。spec 4.5)。
func enrollRecord(egress []string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"egress_allow": egress})
	return b
}

// writeEnrollState は host_keys[] の role: restore_dest と復元先の登録の記録を書く(state.lock の下)。
func writeEnrollState(ctx context.Context, st contract.StateFiles, host, fp string, enrollment json.RawMessage) error {
	h, err := st.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	var hks []hostKeyRec
	if _, err := item(ctx, st, "host_keys", &hks); err != nil {
		return err
	}
	out := []hostKeyRec{}
	for _, k := range hks {
		if !(k.Role == "restore_dest" && k.Host == host) {
			out = append(out, k)
		}
	}
	out = append(out, hostKeyRec{Role: "restore_dest", Host: host, HostKeyFP: fp})
	b, _ := json.Marshal(out)
	if err := st.Update(ctx, h, "host_keys", b); err != nil {
		return err
	}
	return st.Update(ctx, h, "restore_dest_enrollment", enrollment)
}

// Enroll は登録の実行(5.1 の表の Enroller)。WriteState が真(導入時)なら状態を書き、偽(apply の enroll の要素)なら提案の host_key_fp と照らすだけで書かない。
func (e *RestoreDestEnroller) Enroll(ctx context.Context, in contract.EnrollInput) (contract.EnrollResult, error) {
	if e == nil || e.State == nil || e.Adapters == nil {
		return contract.EnrollResult{}, internalErr("")
	}
	if in.Kind != "restore-dest" {
		return contract.EnrollResult{}, argInvalid("--kind", "mismatch")
	}
	var sec struct {
		Adapter string  `json:"adapter"`
		Dest    destRef `json:"dest"`
		SSHFrom string  `json:"ssh_from"`
	}
	d := &defDoc{}
	if json.Unmarshal(in.Section, &sec) != nil || sec.Dest.At == "" {
		return contract.EnrollResult{}, argInvalid("--dest", "format")
	}
	d.Restore = &struct {
		Adapter string  `json:"adapter"`
		Dest    destRef `json:"dest"`
		SSHFrom string  `json:"ssh_from"`
	}{Adapter: sec.Adapter, Dest: sec.Dest, SSHFrom: sec.SSHFrom}
	if d.Restore.Adapter == "" {
		d.Restore.Adapter = "ssh-host"
	}
	host, _, ok := parseDestAt(sec.Dest.At)
	if !ok {
		host = sec.Dest.At
	}
	d.Storage = e.storageOf(ctx)
	out, err := enrollCore(ctx, e.Adapters, e.State, e.Secrets, "", d, in.Admin, in.HostKeyFP)
	if err != nil {
		return contract.EnrollResult{}, err
	}
	rec := enrollRecord(out.Egress)
	if in.WriteState {
		if err := writeEnrollState(ctx, e.State, host, out.FP, rec); err != nil {
			return contract.EnrollResult{}, err
		}
	}
	return contract.EnrollResult{RestoreDestEnroll: rec}, nil
}

// storageOf は保管先の定義(候補があれば候補、無ければ承認済み)。egress_allow の名前解決に使う。読めなければ空。
func (e *RestoreDestEnroller) storageOf(ctx context.Context) []struct {
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket"`
	Prefix   string `json:"prefix"`
	KeyRef   string `json:"key_ref"`
} {
	raw, _, found, err := e.State.ReadCandidate(ctx)
	if err != nil || !found {
		raw, _, err = e.State.ReadDef(ctx)
		if err != nil {
			return nil
		}
	}
	var s defDoc
	_ = json.Unmarshal(raw, &s)
	return s.Storage
}
