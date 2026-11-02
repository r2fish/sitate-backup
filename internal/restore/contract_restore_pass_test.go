package restore

// restore-test の合格経路と不合格経路(作業指示書 5.6 の完了条件 12、spec 3.5 の表の 3〜12)。
// 復元先アダプタは偽の AdapterRunner、復元先への SSH は偽の SSH(contract/fake)、配布物は偽の Bundle。
// 核が復元先で流す台本は順に (1) 隔離の検査 (2) 配置、続けて `sitate restore ...`(Argv)(seams.go の DestProbeOut ほか)。

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

const (
	defPass = `{"version":1,"storage":[{"key_ref":"store-key"}],"restore":{"adapter":"ssh-host","dest":{"at":"ssh://restore@dest.example.invalid:22","key_ref":"dest-key"}}}`
	fpDest  = "SHA256:abcDEF123"
	repoID  = "rp-0123456789abcdef0123456789abcdef"
	privKey = "-----BEGIN OPENSSH PRIVATE KEY-----\nfake-restore-key\n-----END OPENSSH PRIVATE KEY-----\n"
)

// passWorld は合格できる世界。restoreOut は `sitate restore` の標準出力(封筒)と終了状態、isoFail は隔離の検査で落とす項目。
type passWorld struct {
	*env
	SSH           *fake.SSH
	Sec           *fake.SecretStore
	scripts       int
	restoreArgv   []string
	keyLine       string
	stdinAlive    bool
	restoreExit   int
	restoreOut    []byte
	isoFail       string
	placeOverride *contract.DestPlaceOut
	arch          string // 復元先の `uname -m`
	wantArch      string // 配布物の <os>-<arch>
	bundle        *fake.Bundle
	sudo          []bool // 各回の SSHRequest.Sudo
	ds            *strictStore
}

func okRestoreEnvelope() []byte {
	b, _ := json.Marshal(map[string]any{"schema": "sitate.out/v1", "command": []string{"restore"}, "ok": true, "dry_run": false, "run_id": rtRun,
		"result":   map[string]any{"backup_ref": refA, "health": "pass", "files": 3, "bytes": 30, "hash_mismatch": 0, "checks": []any{}, "duration_s": 2, "read_bytes": 4096},
		"warnings": []any{}, "error": nil})
	return b
}

func newPass(t *testing.T) *passWorld { return newPassOpts(t, worldOpts{}) }

// bundleData は <os>-<arch> ごとに違う中身の配布物(復元先の arch を取り違えると、復元先の報告したハッシュと核が送ったもののハッシュが合わない)。
func bundleData(osArch string) map[string][]byte {
	return map[string][]byte{osArch + "/sitate": []byte("core-binary-bytes-" + osArch), osArch + "/adapters/check/files/check": []byte("check-bytes-" + osArch)}
}

func newPassOpts(t *testing.T, o worldOpts) *passWorld {
	t.Helper()
	e := keyringWorld(t, defPass, o)
	e.St.Def = []byte(defPass)
	e.St.Items["host_keys"] = json.RawMessage(`[{"role":"restore_dest","host":"dest.example.invalid","host_key_fp":"` + fpDest + `","session_id":null}]`)
	w := &passWorld{env: e, restoreOut: okRestoreEnvelope(), arch: o.Arch, wantArch: contract.BundleLinuxX86}
	if w.arch == "" {
		w.arch = "x86_64"
	}
	if w.arch == "aarch64" {
		w.wantArch = contract.BundleLinuxArm
	}
	w.Sec = &fake.SecretStore{M: map[string]json.RawMessage{contract.RestoreKeyRef: sshSecret(), "store-key": storageSecret()}}
	w.SSH = &fake.SSH{FP: fpDest}
	w.SSH.Handler = w.handle
	e.Restore.SSH, e.Restore.Secrets = w.SSH, w.Sec
	w.bundle = &fake.Bundle{Version: "0.1.0", FilesBy: map[string][]contract.BundleFile{}}
	for _, a := range []string{contract.BundleLinuxX86, contract.BundleLinuxArm} {
		for p, d := range bundleData(a) {
			w.bundle.FilesBy[a] = append(w.bundle.FilesBy[a], contract.BundleFile{Path: p, Data: d})
		}
	}
	e.Restore.Bundle = w.bundle
	w.ds = e.Restore.Store.(*strictStore)
	e.AR.Script("restore-dest",
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"resources":[],"state":"absent"}`)},
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"session_id":"` + rtRun + `","ssh":{"host":"dest.example.invalid","port":22,"user":"sitate-restore"},"host_key_fp":"` + fpDest + `","scratch":{"path":"/scratch/x","method":"gocryptfs"},"resources":[{"type":"workdir","id":"` + rtRun + `"}]}`)},
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"done":true,"verified_by":"ssh","remaining":[]}`)},
		fake.Step{Outcome: fake.OutcomeSuccess, Result: json.RawMessage(`{"resources":[],"state":"absent"}`)})
	return w
}

func strconv_q(s string) string { b, _ := json.Marshal(s); return string(b) }

func (w *passWorld) handle(ctx context.Context, req contract.SSHRequest) (contract.SSHResult, error) {
	res := contract.SSHResult{SeenFP: fpDest}
	w.sudo = append(w.sudo, req.Sudo)
	if len(req.Argv) > 0 {
		w.restoreArgv = slices.Clone(req.Argv)
		br := bufio.NewReader(req.Stdin)
		w.keyLine, _ = br.ReadString('\n')
		done := make(chan error, 1)
		go func() { _, err := br.ReadByte(); done <- err }()
		select {
		case err := <-done:
			w.stdinAlive = err != io.EOF
		case <-time.After(100 * time.Millisecond):
			w.stdinAlive = true
		}
		_, _ = req.Stdout.Write(w.restoreOut)
		res.ExitStatus = w.restoreExit
		return res, nil
	}
	w.scripts++
	var out any
	if w.scripts == 1 {
		iso := []contract.IsolationProbe{{Name: "inbound", OK: true}, {Name: "outbound", OK: true}, {Name: "dns_name", OK: true}}
		for i := range iso {
			iso[i].OK = iso[i].Name != w.isoFail
		}
		out = contract.DestProbeOut{Isolation: iso, OS: "Linux", Arch: w.arch}
	} else {
		// 復元先が計算した SHA-256 の報告(復元先の arch の配布物のハッシュ。核が違う arch を送ったなら核の側の値と合わない)。
		var po contract.DestPlaceOut
		for p, d := range bundleData(w.wantArch) {
			h := sha256.Sum256(d)
			po.Files = append(po.Files, contract.PlacedFile{Path: p, SHA256: "sha256:" + hex.EncodeToString(h[:])})
		}
		if w.placeOverride != nil {
			po = *w.placeOverride
		}
		out = po
	}
	b, _ := json.Marshal(out)
	_, _ = req.Stdout.Write(b)
	return res, nil
}

func (w *passWorld) journal(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	for _, e := range w.Rec.Entries {
		var h struct {
			Kind string                     `json:"kind"`
			Body map[string]json.RawMessage `json:"body"`
		}
		_ = json.Unmarshal(e, &h)
		if h.Kind == "restore_test" {
			return h.Body
		}
	}
	t.Fatalf("Recorder に kind: restore_test の項目が無い: %d 件", len(w.Rec.Entries))
	return nil
}

func (w *passWorld) adapterOps() []string {
	var ops []string
	for _, c := range w.AR.Calls {
		var r adReq
		_ = json.Unmarshal(c.Req, &r)
		ops = append(ops, r.Op)
	}
	return ops
}

// 合格: proof が sitate.restore-proof/v1 の形で保持区分と保持期限を持ち、Crypto で署名される。checks[] は結果も journal も空の配列。
// 鍵は隔離の検査と配置の後に標準入力で渡し、標準入力は復元が終わるまで開けたまま。終わったら stop と inspect で破棄を確かめる。
func TestContractRestoreTestProof(t *testing.T) {
	w := newPass(t)
	rep, err := w.RestoreTest(ctx, rtCall(false))
	harness.Shape(t, []string{"restore-test"}, rep, err)
	if err != nil {
		return
	}
	m := resultOf(t, rep, "restore_test")
	wantRaw(t, m, "restore_ok", "true")
	wantRaw(t, m, "checks", "[]")
	wantRaw(t, m, "deferred", "false")
	wantRaw(t, m, "keyring_ok", "true")     // 表の 1: 省いて null を返す実装は落ちる
	wantRaw(t, m, "config_copy_ok", "true") // 表の 2
	wantRaw(t, m, "backup_ref", `"`+refA+`"`)
	if string(m["proof"]) == "null" {
		t.Fatal("合格なのに proof が無い")
	}
	checkProofBody(t, w, m["proof"])
	// journal の restore_test の項目: checks[] は空の配列(欄が無いのではない)、proof は結果と同じ。
	jb := w.journal(t)
	if string(jb["checks"]) != "[]" {
		t.Errorf("journal の restore_test の checks[] は空の配列: %s", jb["checks"])
	}
	if string(jb["ok"]) != "true" || string(jb["proof"]) == "null" || len(jb["proof"]) == 0 {
		t.Errorf("journal の restore_test の ok・proof: %s", jb)
	}
	checkRestoreSSH(t, w)
	if ops := w.adapterOps(); !slices.Equal(ops, []string{"inspect", "start", "stop", "inspect"}) {
		t.Errorf("復元先アダプタの呼び出し = %v (掃引・start・stop・inspect)", ops)
	}
	if len(w.St.Ledger) != 0 {
		t.Errorf("破棄を確かめたら台帳の記録を消す: %s", w.St.Ledger)
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("警告が出た: %+v", rep.Warnings)
	}
}

// 不合格(復元先の restore が RESTORE_MISMATCH): proof を持たず、journal の restore_test も ok: false・proof: null・checks は空の配列。
// 破棄は成否にかかわらず行い、世代に「復元できなかった」印を付ける。
func TestContractRestoreTestFailNoProof(t *testing.T) {
	w := newPass(t)
	env, _ := json.Marshal(map[string]any{"schema": "sitate.out/v1", "command": []string{"restore"}, "ok": false, "dry_run": false, "run_id": rtRun, "result": nil, "warnings": []any{},
		"error": map[string]any{"code": "RESTORE_MISMATCH", "class": "verification", "retryable": false, "detail": map[string]any{"hash_mismatch": []any{}, "files_ok": 2, "files_failed": 1}, "next": []any{}}})
	w.restoreOut, w.restoreExit = env, 6
	rep, err := w.RestoreTest(ctx, rtCall(false))
	harness.Implemented(t, err)
	ce := harness.ExpectCode(t, err, contract.CodeRestoreMismatch)
	_ = ce
	if len(rep.Result) != 0 && strings.Contains(string(rep.Result), `"proof":{`) {
		t.Errorf("不合格が proof を持つ: %s", rep.Result)
	}
	jb := w.journal(t)
	if string(jb["ok"]) != "false" || string(jb["proof"]) != "null" || string(jb["checks"]) != "[]" {
		t.Errorf("journal の restore_test: ok=%s proof=%s checks=%s", jb["ok"], jb["proof"], jb["checks"])
	}
	if ops := w.adapterOps(); len(ops) < 3 || ops[len(ops)-2] != "stop" {
		t.Errorf("不合格でも stop と inspect で破棄する: %v", ops)
	}
	var snaps []map[string]any
	_ = json.Unmarshal(w.St.Items["snapshots"], &snaps)
	for _, s := range snaps {
		if s["ref"] == refA && s["restore_failed"] != true {
			t.Errorf("RESTORE_MISMATCH で世代に「復元できなかった」印を付けていない: %v", s)
		}
	}
}

// 隔離の検査が落ちたら鍵を渡さず RESTORE_ISOLATION_FAILED。配置も restore もせず、破棄する。proof なし。
func TestContractRestoreTestIsolationFailed(t *testing.T) {
	for _, bad := range []string{"inbound", "outbound", "dns_name"} {
		w := newPass(t)
		w.isoFail = bad
		_, err := w.RestoreTest(ctx, rtCall(false))
		harness.ExpectCode(t, err, contract.CodeRestoreIsolationFailed)
		if n := len(w.SSH.Recorded()); n != 1 {
			t.Errorf("%s: 隔離の検査の後に進んだ: SSH %d 回", bad, n)
		}
		for _, c := range w.SSH.Recorded() {
			if bytes.Contains(c.Stdin, []byte("snap-key")) || w.keyLine != "" {
				t.Errorf("%s: 鍵を渡した", bad)
			}
		}
		if ops := w.adapterOps(); len(ops) < 3 || ops[len(ops)-2] != "stop" {
			t.Errorf("%s: 破棄していない: %v", bad, ops)
		}
	}
}

// 見えたホスト鍵が固定値と違えば RESTORE_HOSTKEY_MISMATCH(expected_fp・seen_fp)。何も流さず鍵を渡さず、破棄する。
func TestContractRestoreTestHostKeyMismatch(t *testing.T) {
	w := newPass(t)
	w.SSH.FP = "SHA256:otherOther"
	_, err := w.RestoreTest(ctx, rtCall(false))
	ce := harness.ExpectCode(t, err, contract.CodeRestoreHostkeyMismatch)
	var d struct {
		ExpectedFP string `json:"expected_fp"`
		SeenFP     string `json:"seen_fp"`
		SessionID  any    `json:"session_id"`
	}
	_ = json.Unmarshal(ce.Detail, &d)
	if d.ExpectedFP != fpDest || d.SeenFP != "SHA256:otherOther" {
		t.Errorf("detail: %s", ce.Detail)
	}
	if err := contract.Validate("cli/detail.json#/$defs/RESTORE_HOSTKEY_MISMATCH", ce.Detail); err != nil {
		t.Errorf("detail がスキーマに合わない: %v", err)
	}
	if w.keyLine != "" || w.scripts != 0 {
		t.Error("ホスト鍵が違うのに台本か鍵を流した")
	}
	if ops := w.adapterOps(); len(ops) < 3 || ops[len(ops)-2] != "stop" {
		t.Errorf("破棄していない: %v", ops)
	}
}

// 配置で復元先が計算した SHA-256 がバックアップサーバー側と違えば中止し、鍵を渡さない。
func TestContractRestoreTestPlacementMismatch(t *testing.T) {
	w := newPass(t)
	w.placeOverride = &contract.DestPlaceOut{Files: []contract.PlacedFile{{Path: contract.BundleLinuxX86 + "/sitate", SHA256: "sha256:" + strings.Repeat("0", 64)}}}
	_, err := w.RestoreTest(ctx, rtCall(false))
	harness.Implemented(t, err)
	if err == nil {
		t.Fatal("配置の照合が外れたのに成功した")
	}
	if w.keyLine != "" {
		t.Error("配置の照合が外れたのに鍵を渡した")
	}
}

func checkProofSig(t *testing.T, raw json.RawMessage, sigB64 string) {
	t.Helper()
	var proof map[string]json.RawMessage
	_ = json.Unmarshal(raw, &proof)
	delete(proof, "sig")
	body, _ := json.Marshal(proof)
	canon, _ := contract.CanonicalJSON(body)
	sig, derr := base64.StdEncoding.DecodeString(sigB64)
	if ok, _ := (fake.Crypto{}).VerifyProof(canon, sig, nil); derr != nil || !ok {
		t.Errorf("sig が sig を除いた記録の正規化への署名でない(RFC 8785 直列化に掛ける): %v", derr)
	}
}

func checkProofBody(t *testing.T, w *passWorld, raw json.RawMessage) {
	t.Helper()
	var pj struct {
		RepoID         string `json:"repo_id"`
		BackupRef      string `json:"backup_ref"`
		StoredAt       string `json:"stored_at"`
		RetentionClass string `json:"retention_class"`
		RetentionUntil string `json:"retention_until"`
		KeyID          string `json:"key_id"`
		Sig            string `json:"sig"`
	}
	_ = json.Unmarshal(raw, &pj)
	var sn struct {
		StoredAt, RetentionClass, RetentionUntil string
	}
	var snaps []map[string]any
	_ = json.Unmarshal(w.St.Items["snapshots"], &snaps)
	for _, s := range snaps {
		if s["ref"] == refA {
			sn.StoredAt, sn.RetentionClass, sn.RetentionUntil = s["stored_at"].(string), s["retention_class"].(string), s["retention_until"].(string)
		}
	}
	if pj.RepoID != repoID || pj.BackupRef != refA || pj.StoredAt != sn.StoredAt || pj.RetentionClass != "monthly" || pj.RetentionUntil != sn.RetentionUntil || pj.KeyID == "" {
		t.Errorf("proof の中身が世代の記録と合わない: %s (世代 %+v)", raw, sn)
	}
	checkProofSig(t, raw, pj.Sig)
}

func checkRestoreSSH(t *testing.T, w *passWorld) {
	t.Helper()
	// 復元先への SSH: 隔離の検査 → 配置 → restore の 3 回。どれも固定した指紋で、復元先用の鍵を資格情報にする。
	calls := w.SSH.Recorded()
	if len(calls) != 3 {
		t.Fatalf("SSH は 3 回(隔離の検査・配置・restore): %d 回", len(calls))
	}
	for i, c := range calls {
		if c.Req.PinnedFP != fpDest || c.Cred != privKey || c.Req.Target.Host != "dest.example.invalid" || c.Req.Target.User != "sitate-restore" {
			t.Errorf("SSH %d 回目の接続の指定: %+v", i+1, c.Req.SSHConn)
		}
	}
	// 配置の本体の符号化は試験が要求しない(base64 でも生でもよい)。復元先の arch の配布物を取ったこと、復元先が報告したハッシュで照合したことを見る。
	if got := w.bundle.Asked; len(got) == 0 || got[len(got)-1] != w.wantArch {
		t.Errorf("配布物を復元先の <os>-<arch> %s で引いていない: %v", w.wantArch, got)
	}
	for i, s := range w.sudo {
		if s {
			t.Errorf("SSH %d 回目が Sudo: 真(sitate-restore は root も sudo も持たない。spec 3.5)", i+1)
		}
	}
	if w.ds.blind != 0 {
		t.Errorf("版 ID なしの読み出しが %d 回(記録した版 ID を指定する)", w.ds.blind)
	}
	for _, c := range calls[:2] {
		if bytes.Contains(c.Stdin, []byte("snap-key")) || bytes.Contains(c.Stdin, []byte("sR")) {
			t.Error("鍵を渡す前の段に鍵が出ている")
		}
	}
	argv := strings.Join(w.restoreArgv, " ")
	for _, want := range []string{"sitate restore", "--backup " + refA, "--snapshot-key-stdin", "--to /scratch/x", "--checks all"} {
		if !strings.Contains(argv, want) {
			t.Errorf("restore の引数列に %q が無い: %s", want, argv)
		}
	}
	var kl contract.DestKeyLine
	if json.Unmarshal([]byte(w.keyLine), &kl) != nil || kl.ReadKey.KeyID != "kR" || kl.ReadKey.Secret != "sR" {
		t.Errorf("標準入力の 1 行目に読み取り鍵が無い: %q", w.keyLine)
	}
	if k, _ := base64.StdEncoding.DecodeString(kl.SnapshotKey); string(k) != "snap-key" {
		t.Errorf("標準入力の 1 行目にスナップショット鍵が無い: %q", w.keyLine)
	}
	if strings.Contains(w.keyLine, "sW") {
		t.Error("書き込み鍵を復元先へ渡した")
	}
	if !w.stdinAlive {
		t.Error("鍵を渡した後に標準入力を閉じた(終わりは取り消しの合図)")
	}
}
