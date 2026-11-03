package restore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// destConn は復元先への接続の指定(固定した指紋・核の復元先用の鍵)。
func (r *Restore) destConn(ctx context.Context, host string, port int, user, pinned string) contract.SSHConn {
	var cred *contract.Secret
	if r.Secrets != nil {
		if raw, err := r.Secrets.Get(ctx, contract.RestoreKeyRef); err == nil {
			var k contract.SSHKeySecret
			if json.Unmarshal(raw, &k) == nil {
				cred = contract.NewSecret([]byte(k.PrivateKey))
			}
		}
	}
	return contract.SSHConn{Target: contract.SSHTarget{Host: host, Port: port, User: user}, CredRef: contract.RestoreKeyRef, Cred: cred, PinnedFP: pinned}
}

// sshErr は SSH の失敗を原因コードに写す(spec 3.5、3.1.3)。届かない = STORAGE_UNREACHABLE(restore_host)、資格情報の拒否 = RESTORE_HOST_DENIED、
// 固定した指紋と違う = RESTORE_HOSTKEY_MISMATCH(セッションの無い ssh-host は session_id: null)。
func sshErr(err error) error {
	var hk *contract.SSHHostKeyError
	var se *contract.SSHError
	switch {
	case errors.As(err, &hk):
		return newErr(contract.CodeRestoreHostkeyMismatch, map[string]any{"expected_fp": hk.Pinned, "seen_fp": hk.Seen, "session_id": nil})
	case errors.As(err, &se) && se.Reason == contract.SSHReasonAuth:
		return newErr(contract.CodeRestoreHostDenied, map[string]any{"reason": "auth"})
	case errors.As(err, &se) && (se.Reason == contract.SSHReasonConnect || se.Reason == contract.SSHReasonTimeout):
		return newErr(contract.CodeStorageUnreachable, map[string]any{"target": "restore_host", "reason": se.Reason, "http_status": nil})
	case errors.As(err, &se):
		return internalErr("contract")
	}
	return err
}

// runScript は復元先で台本を流し、標準出力の JSON を out に読む(sudo は使わない: sitate-restore は root も sudo も持たない)。
func (r *Restore) runScript(ctx context.Context, conn contract.SSHConn, script []byte, out any) (string, error) {
	if r.SSH == nil {
		return "", internalErr("")
	}
	buf := &limitBuf{max: maxOutput}
	res, err := r.SSH.Run(ctx, contract.SSHRequest{SSHConn: conn, Script: script, Stdout: buf})
	if err != nil {
		return res.SeenFP, sshErr(err)
	}
	if buf.over || res.ExitStatus != 0 || json.Unmarshal(buf.Bytes(), out) != nil {
		return res.SeenFP, internalErr(map[bool]string{true: "exit_code", false: "output"}[res.ExitStatus != 0])
	}
	return res.SeenFP, nil
}

// isoItem は結果の isolation[] の要素。
type isoItem struct {
	Name string  `json:"name"`
	OK   bool    `json:"ok"`
	Code *string `json:"code"`
}

// isolationItems は検査の結果を inbound・outbound・dns_name の 3 項目にする(通った項目は code: null、落ちた項目は RESTORE_ISOLATION_FAILED)。
// 報告に無い項目は落ちたものとして扱う。
func isolationItems(out contract.DestProbeOut) (items []isoItem, failed []string) {
	got := map[string]bool{}
	for _, p := range out.Isolation {
		got[p.Name] = p.OK
	}
	items, failed = []isoItem{}, []string{}
	for _, n := range []string{"inbound", "outbound", "dns_name"} {
		it := isoItem{Name: n, OK: got[n]}
		if !it.OK {
			c := string(contract.CodeRestoreIsolationFailed)
			it.Code = &c
			failed = append(failed, n)
		}
		items = append(items, it)
	}
	return items, failed
}

// maxOutput は復元先の標準出力の上限(信頼しない相手の出力でメモリを使い切られないため)。
const maxOutput = 1 << 20

// limitBuf は上限までためる Writer。超えたら over を立てて以降を捨てる(書き込みは成功として返し、呼び出し側が over を見る)。
type limitBuf struct {
	bytes.Buffer
	max  int
	over bool
}

func (l *limitBuf) Write(p []byte) (int, error) {
	if l.Len()+len(p) > l.max {
		l.over = true
		return len(p), nil
	}
	return l.Buffer.Write(p)
}
