package restore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// remoteOut は復元先の restore の結果(成功の封筒の result)。
type remoteOut struct {
	Files     int64
	Bytes     int64
	ReadBytes int64
	Health    string
	Started   bool
	Teardown  *teardownResult
	Done      bool
}

// provision は表の 3: 掃引の後、台帳に session_id を書いてから start を呼ぶ。返った資源は台帳に足す。
func (t *rtState) provision(ctx context.Context) error {
	host, port, ok := parseDestAt(t.d.Restore.Dest.At)
	if !ok {
		return argInvalid("--dest", "format")
	}
	fp, err := pinnedFP(ctx, t.r.State, host)
	if err != nil {
		return err
	}
	if fp == "" { // 登録の無い復元先: ホスト鍵を照らせないので、鍵を渡す前(start の前)に止める(spec 3.5 の表の 4)
		return newErr(contract.CodeRestoreEnrollMismatch, enrollMismatchDetail("enroll_mismatch", "", ""))
	}
	if t.d.Restore.SSHFrom == "" { // 送信元を観測する口が無い間は、受信を許す送信元を決められないので呼ばない(spec 3.2.4)
		return defMissing("/restore/ssh_from")
	}
	if err := sweep(ctx, t.r.Adapters, t.r.State, t.r.Secrets, t.runID, t.d.Restore.Adapter, t.d.Restore.Dest, fp, t.start); err != nil {
		return err
	}
	if err := t.settleLeftovers(ctx); err != nil {
		return err
	}
	rec := ledgerRec{RecordID: newRecordID(), Kind: "dest_session", RunID: t.runID, Adapter: t.d.Restore.Adapter, Dest: t.d.Restore.Dest, Purpose: "restore_test",
		SessionID: t.runID, State: "creating", Resources: []resRef{}, SSH: sshRef{Host: host, Port: port, User: restoreUser}, HostKeyFP: &fp, BackupRef: t.snap.Ref}
	if err := ledgerPut(ctx, t.r.State, rec); err != nil {
		return err
	}
	t.rec = &rec
	return t.start2(ctx, fp)
}

// deferErr は新しい資源を作らずに次回へ送ること(前の破棄の残存など。結果は deferred: true)。
type deferErr struct {
	reason string
	ws     []contract.Warning
}

func (d *deferErr) Error() string { return "deferred: " + d.reason }

func defMissing(pointer string) error {
	return newErr(contract.CodeDefInvalid, map[string]any{"issues": []map[string]string{{"pointer": pointer, "reason": "missing"}}})
}

// settleLeftovers は掃引の後に台帳を読み直し、同じ session_id の前の試みの記録が残っていれば片付けを試み、
// 破棄を確かめられない残存(destroy_failed)や開いた復元セッションがあれば新しい資源を作らずに次回へ送る(spec 3.2.4 の session_id、3.5)。
func (t *rtState) settleLeftovers(ctx context.Context) error {
	recs, err := readLedger(ctx, t.r.State)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if rec.SessionID != t.runID || !(rec.State == "creating" || rec.State == "created") {
			continue
		}
		if tr := teardownSession(ctx, t.r.Adapters, t.r.Secrets, rec); tr.Absent {
			if err := ledgerDelete(ctx, t.r.State, rec.RecordID); err != nil {
				return err
			}
		} else if _, err := markFailed(ctx, t.r.State, rec, tr, t.start); err != nil {
			return err
		}
	}
	if recs, err = readLedger(ctx, t.r.State); err != nil {
		return err
	}
	if reason, ws := deferral(recs); reason != "" {
		return &deferErr{reason, ws}
	}
	return nil
}

// start2 は復元先アダプタの start を呼び、結果(または失敗の resources)を台帳へ足す。
func (t *rtState) start2(ctx context.Context, fp string) error {
	key, _ := restoreKey(ctx, t.r.Secrets, false)
	egress, _ := t.egressFixed(ctx)
	allow := []string{}
	if t.d.Restore.SSHFrom != "" {
		allow = append(allow, t.d.Restore.SSHFrom)
	}
	raw, err := destCall(ctx, t.r.Adapters, t.runID, t.d.Restore.Adapter, map[string]any{"op": "start", "session_id": t.runID, "dest": t.d.Restore.Dest,
		"needs": map[string]any{"disk_gb": t.needGB()}, "host_key_fp": fp, "authorized_key": key.PublicKey, "allow_ssh_from": allow, "egress_allow": egress,
		"egress_host": t.d.endpointHost(), "secrets": map[string]string{"restore-dest-ssh": key.PrivateKey}})
	if err != nil {
		bg := context.WithoutCancel(ctx)
		var ce *contract.Error
		switch rs := errResources(err); {
		case len(rs) > 0:
			t.rec.Resources, t.rec.State = nonNilRes(rs), "created"
			_ = ledgerPut(bg, t.r.State, *t.rec)
		case errors.As(err, &ce) && ce.Code == contract.CodeAdapterError && ce.Adapter != nil:
			// 終了 1 で error.resources[] が空: 何も作っていない。消してよいのはこの場合だけ(打ち切り・応答の喪失・取り消しは表の 11 で破棄を確かめる)
			_ = ledgerDelete(bg, t.r.State, t.rec.RecordID)
			t.rec = nil
		}
		return mapDestErr("start", err, fp)
	}
	var res struct {
		SSH       sshRef                `json:"ssh"`
		FP        string                `json:"host_key_fp"`
		Scratch   struct{ Path string } `json:"scratch"`
		Resources []resRef              `json:"resources"`
	}
	if json.Unmarshal(raw, &res) != nil || res.Scratch.Path == "" {
		return internalErr("output")
	}
	t.rec.Resources, t.rec.State, t.rec.SSH = nonNilRes(res.Resources), "created", res.SSH
	if res.FP != "" {
		t.rec.HostKeyFP = &res.FP
	}
	if err := ledgerPut(ctx, t.r.State, *t.rec); err != nil {
		return err
	}
	t.scratch = res.Scratch.Path
	return nil
}

// needGB は復元先に要る空き(GB)。マニフェストの対象の大きさの合計を切り上げる(最小 1)。
func (t *rtState) needGB() int64 {
	var sum int64
	for _, x := range t.man.Targets {
		sum += x.Bytes
	}
	const gb = int64(1) << 30
	n := (sum + gb - 1) / gb
	if n < 1 {
		n = 1
	}
	return n
}

// egressFixed は登録時に固定した送信の許可先(state.json の restore_dest_enrollment)。start に渡す(解決し直さない。spec 3.2.4)。
func (t *rtState) egressFixed(ctx context.Context) ([]string, error) {
	var e struct {
		EgressAllow []string `json:"egress_allow"`
	}
	if _, err := item(ctx, t.r.State, "restore_dest_enrollment", &e); err != nil {
		return nil, err
	}
	if e.EgressAllow == nil {
		e.EgressAllow = []string{}
	}
	return e.EgressAllow, nil
}

// remoteRestore は表の 4〜8: ホスト鍵の照合、隔離の検査、核とチェックの配置、鍵の受け渡しと復元先の restore。
func (t *rtState) remoteRestore(ctx context.Context) error {
	fp := ""
	if t.rec.HostKeyFP != nil {
		fp = *t.rec.HostKeyFP
	}
	pinned, err := pinnedFP(ctx, t.r.State, t.rec.SSH.Host)
	if err != nil {
		return err
	}
	if pinned == "" || fp != pinned {
		return newErr(contract.CodeRestoreHostkeyMismatch, map[string]any{"expected_fp": pinned, "seen_fp": fp, "session_id": nil})
	}
	conn := t.r.destConn(ctx, t.rec.SSH.Host, t.rec.SSH.Port, t.rec.SSH.User, pinned)
	var po contract.DestProbeOut
	if _, err := t.r.runScript(ctx, conn, probeScript(t.d.forbiddenNames(), t.rec.SSH.Port), &po); err != nil {
		return err
	}
	if _, failed := isolationItems(po); len(failed) > 0 {
		return newErr(contract.CodeRestoreIsolationFailed, map[string]any{"failed": failed})
	}
	osArch, ok := contract.BundleOSArch(po.OS, po.Arch)
	if !ok {
		return newErr(contract.CodeRestoreEnrollMismatch, enrollMismatchDetail("unsupported_platform", po.OS, po.Arch))
	}
	if err := t.place(ctx, conn, osArch); err != nil {
		return err
	}
	return t.runRestore(ctx, conn, osArch)
}

// place は核とチェック一式を送って置き、復元先で計算した SHA-256 をバックアップサーバー側の値と照らす。違えば鍵を渡さずに中止する。
func (t *rtState) place(ctx context.Context, conn contract.SSHConn, osArch string) error {
	if t.r.Bundle == nil {
		return internalErr("")
	}
	_, files, err := t.r.Bundle.Files(ctx, osArch)
	if err != nil {
		return err
	}
	script, err := placeScript(t.runID, files)
	if err != nil {
		return err
	}
	var po contract.DestPlaceOut
	if _, err := t.r.runScript(ctx, conn, script, &po); err != nil {
		return err
	}
	got := map[string]string{}
	for _, f := range po.Files {
		got[f.Path] = f.SHA256
	}
	for _, f := range files {
		h := sha256.Sum256(f.Data)
		if got[f.Path] != "sha256:"+hex.EncodeToString(h[:]) {
			return internalErr("output")
		}
	}
	return nil
}

// readKeyEntry は保管先の読み取り鍵(定義の storage[0].key_ref の storage_keys の role: read)。
func (t *rtState) readKey(ctx context.Context) (id, secret string, err error) {
	if t.r.Secrets == nil || len(t.d.Storage) == 0 {
		return "", "", internalErr("")
	}
	raw, err := t.r.Secrets.Get(ctx, t.d.Storage[0].KeyRef)
	if err != nil {
		return "", "", err
	}
	var k contract.StorageKeysSecret
	if json.Unmarshal(raw, &k) != nil {
		return "", "", internalErr("")
	}
	for _, e := range k.Keys {
		if e.Role == "read" {
			return e.KeyID, e.Secret, nil
		}
	}
	return "", "", newErr(contract.CodeSecretMissing, map[string]any{"ref": t.d.Storage[0].KeyRef})
}

// runRestore は表の 7・8: 鍵を標準入力の 1 行目で渡し(ディスクに書かない)、標準入力を開けたまま復元先の restore を実行する。
// 標準入力の終わり(SSH の切断)は取り消し。読み出しを始める直前に、見込みの量を耐久記録する(強制終了の精算の材料)。
func (t *rtState) runRestore(ctx context.Context, conn contract.SSHConn, osArch string) error {
	kid, ksec, err := t.readKey(ctx)
	if err != nil {
		return err
	}
	var line contract.DestKeyLine
	line.SnapshotKey = base64.StdEncoding.EncodeToString(t.snapKey)
	line.ReadKey.KeyID, line.ReadKey.Secret = kid, ksec
	lb, _ := json.Marshal(line)
	lb = append(lb, '\n')
	defer func() {
		for i := range lb {
			lb[i] = 0
		}
	}()
	st := t.d.Storage
	var ep, bk, px string
	if len(st) > 0 {
		ep, bk, px = st[0].Endpoint, st[0].Bucket, st[0].Prefix
	}
	if px == "" {
		px = contract.StoragePrefixDefault // 定義の既定(spec 4.1)。空の語は通信路で消えるので渡さない
	}
	t.readStarted = true // 復元先が読み出しを始める(印の付け方の判定に使う)
	argv := []string{runDir(t.runID) + "/" + osArch + "/sitate", "restore", "--backup", t.snap.Ref, "--endpoint", ep, "--bucket", bk, "--prefix", px,
		"--snapshot-key-stdin", "--to", t.scratch, "--checks", "all"}
	pr, pw := io.Pipe()
	go func() { _, _ = pw.Write(lb) }()
	out := &limitBuf{max: maxOutput}
	res, err := t.r.SSH.Run(ctx, contract.SSHRequest{SSHConn: conn, Argv: argv, Stdin: pr, Stdout: out})
	_ = pw.Close()
	if err != nil {
		if ctx.Err() != nil {
			return abortError(ctx)
		}
		return sshErr(err)
	}
	if out.over {
		return internalErr("output")
	}
	if err := t.parseRestoreOut(out.Bytes(), res.ExitStatus); err != nil {
		return err
	}
	return t.checkAgainstManifest()
}

// parseRestoreOut は復元先の restore の封筒を読む。失敗の封筒はその原因コードと detail のまま返す。
func (t *rtState) parseRestoreOut(b []byte, exit int) error {
	var env struct {
		OK     bool `json:"ok"`
		Result struct {
			Files     int64  `json:"files"`
			Bytes     int64  `json:"bytes"`
			Health    string `json:"health"`
			ReadBytes int64  `json:"read_bytes"`
			Mismatch  int    `json:"hash_mismatch"`
		} `json:"result"`
		Error *remoteError `json:"error"`
	}
	if json.Unmarshal(b, &env) != nil {
		return internalErr("output")
	}
	if !env.OK {
		return remoteFailure(env.Error)
	}
	if exit != 0 || env.Result.Mismatch != 0 {
		return internalErr("exit_code")
	}
	t.remote = remoteOut{Files: env.Result.Files, Bytes: env.Result.Bytes, ReadBytes: env.Result.ReadBytes, Health: env.Result.Health, Done: true}
	return nil
}

type remoteError struct {
	Code   contract.Code   `json:"code"`
	Detail json.RawMessage `json:"detail"`
}

// remoteFailure は復元先の失敗の封筒の error を、受けてよいコード(表の 8・9)に限って自分の誤りにする。
// 復元先は信頼しない: detail は原因コードのスキーマに合うものだけを通し、合わなければ INTERNAL(output)。
func remoteFailure(e *remoteError) error {
	if e == nil {
		return internalErr("output")
	}
	switch e.Code {
	case contract.CodeRestoreMismatch, contract.CodeDecryptFailed, contract.CodeCheckFailed, contract.CodeFormatTooNew, contract.CodeFormatTooOld:
	default:
		return internalErr("output")
	}
	if len(e.Detail) == 0 || contract.Validate("cli/detail.json#/$defs/"+string(e.Code), e.Detail) != nil {
		return internalErr("output")
	}
	out := newErr(e.Code, nil)
	out.Detail = e.Detail
	return out
}

// checkAgainstManifest は復元先の報告(件数と大きさ)を、マニフェストの対象の記録(files・dumps・bytes の和)と比べる。
// 復元先の自己申告だけで合格にして署名しない(0 件の「成功」を通さない)。
func (t *rtState) checkAgainstManifest() error {
	var files, bytes int64
	for _, x := range t.man.Targets {
		files += x.Files + int64(len(x.Dumps))
		bytes += x.Bytes
	}
	if t.remote.Files == files && t.remote.Bytes == bytes {
		return nil
	}
	diff := files - t.remote.Files
	if diff < 0 {
		diff = -diff
	}
	if diff == 0 {
		diff = 1
	}
	ok := min(files, t.remote.Files)
	return newErr(contract.CodeRestoreMismatch, map[string]any{"hash_mismatch": []any{}, "files_ok": ok, "files_failed": diff})
}
