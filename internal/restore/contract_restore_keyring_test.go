package restore

// restore-test の表の 1(キーリングの照合)・表の 2(config の副本の照合)、復元先の arch(表の 6)の契約試験。
// 期待は spec 3.5 の文から書く: 1 は保管先のキーリングの最新版を記録した版 ID で読み、ハッシュを最後に phrase verify で開けた版と照らし、
// 保持中のマニフェストが参照する鍵 ID が含まれることを確かめる。2 は config/<def_hash> を記録した版 ID で読み、正規化のハッシュを照らす。

import (
	"encoding/json"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func noRemote(t *testing.T, w *passWorld, what string) {
	t.Helper()
	if n := len(w.AR.Calls); n != 0 {
		t.Errorf("%s: 復元先アダプタを %d 回呼んだ(復元先は作らない)", what, n)
	}
	if n := len(w.SSH.Recorded()); n != 0 {
		t.Errorf("%s: SSH %d 回", what, n)
	}
	jb := w.journal(t)
	if string(jb["ok"]) != "false" || string(jb["proof"]) != "null" {
		t.Errorf("%s: journal の restore_test は ok: false・proof: null: %s", what, jb)
	}
}

// 保管先のキーリングが、最後に開けた版と違えば KEYRING_MISMATCH(すり替えの検出。T-20)。
func TestContractRestoreTestKeyringHashMismatch(t *testing.T) {
	w := newPassOpts(t, worldOpts{KeyringTamper: true})
	_, err := w.RestoreTest(ctx, rtCall(false))
	harness.ExpectCode(t, err, contract.CodeKeyringMismatch)
	noRemote(t, w, "キーリングのハッシュ違い")
	if w.ds.blind != 0 {
		t.Errorf("版 ID なしの読み出し %d 回", w.ds.blind)
	}
}

// 保持中のマニフェストが、キーリングの最新版に無いマスター鍵の鍵 ID を参照するなら KEYRING_MISMATCH。
func TestContractRestoreTestKeyringMissingKeyID(t *testing.T) {
	w := newPassOpts(t, worldOpts{UnknownKeyID: true})
	_, err := w.RestoreTest(ctx, rtCall(false))
	harness.ExpectCode(t, err, contract.CodeKeyringMismatch)
	noRemote(t, w, "鍵 ID が無い")
}

// config の副本が無い・合わないときは警告 CONFIG_COPY_MISMATCH で、復元テストは続けて合格する(config_copy_ok: false)。
func TestContractRestoreTestConfigCopyMismatchWarns(t *testing.T) {
	for name, o := range map[string]worldOpts{"副本なし": {NoConfig: true}, "中身が合わない": {ConfigTamper: true}} {
		w := newPassOpts(t, o)
		rep, err := w.RestoreTest(ctx, rtCall(false))
		harness.Shape(t, []string{"restore-test"}, rep, err)
		if err != nil {
			continue
		}
		m := resultOf(t, rep, "restore_test")
		wantRaw(t, m, "restore_ok", "true")
		wantRaw(t, m, "config_copy_ok", "false")
		wantRaw(t, m, "keyring_ok", "true")
		if len(rep.Warnings) != 1 || rep.Warnings[0].Code != contract.CodeConfigCopyMismatch {
			t.Errorf("%s: 警告 CONFIG_COPY_MISMATCH が 1 つのはず: %+v", name, rep.Warnings)
		}
	}
}

// 復元先の arch(uname -m)で配布物の <os>-<arch> を決める。arm64 の復元先には linux-arm64 を送る(x86_64 の核を送らない)。
func TestContractRestoreTestDestArch(t *testing.T) {
	for _, arch := range []string{"x86_64", "aarch64"} {
		w := newPassOpts(t, worldOpts{Arch: arch})
		rep, err := w.RestoreTest(ctx, rtCall(false))
		harness.Shape(t, []string{"restore-test"}, rep, err)
		if err != nil {
			continue
		}
		wantRaw(t, resultOf(t, rep, "restore_test"), "restore_ok", "true")
		if a := w.bundle.Asked; len(a) == 0 || a[len(a)-1] != w.wantArch {
			t.Errorf("uname -m %s: 配布物を %v で引いた(%s のはず)", arch, a, w.wantArch)
		}
	}
}

// 対応しない arch の復元先には配置せず鍵を渡さない(中止して破棄する)。
func TestContractRestoreTestDestArchUnsupported(t *testing.T) {
	w := newPassOpts(t, worldOpts{Arch: "riscv64"})
	_, err := w.RestoreTest(ctx, rtCall(false))
	ce := harness.ExpectCode(t, err, contract.CodeRestoreEnrollMismatch)
	var d struct{ Reason, OS, Arch string }
	if json.Unmarshal(ce.Detail, &d) != nil || d.Reason != "unsupported_platform" || d.OS != "Linux" || d.Arch != "riscv64" {
		t.Errorf("detail: %s", ce.Detail)
	}
	if w.scripts != 1 || w.keyLine != "" {
		t.Errorf("隔離の検査の後に進んだ: 台本 %d 回, 鍵 %q", w.scripts, w.keyLine)
	}
	if ops := w.adapterOps(); len(ops) < 3 || ops[len(ops)-2] != "stop" {
		t.Errorf("破棄していない: %v", ops)
	}
}

// restore.dest.at は adapter が ssh-host なら ssh://<user>@<host>:<port> の形(spec 3.5、4.1)。fixture の形は通り、形でない値は拒む。
func TestContractRestoreDestAtShape(t *testing.T) {
	sec := func(adapter, at string) []byte {
		return []byte(`{"adapter":"` + adapter + `","dest":{"at":"` + at + `","key_ref":"dest-key"}}`)
	}
	const sch = "data/definition.json#/$defs/definition/properties/restore"
	for _, at := range []string{destURL, "ssh://restore@dest.example.invalid", "ssh://restore@[2001:db8::1]:22"} {
		if err := contract.Validate(sch, sec("ssh-host", at)); err != nil {
			t.Errorf("ssh-host の at %q を拒んだ: %v", at, err)
		}
	}
	for _, at := range []string{"dest.example.invalid", "ssh://dest.example.invalid", "http://restore@dest.example.invalid:22", "ssh://restore@"} {
		if contract.Validate(sch, sec("ssh-host", at)) == nil {
			t.Errorf("ssh-host の at %q を通した", at)
		}
	}
	if err := contract.Validate(sch, sec("other-adapter", "vm-id-1234")); err != nil {
		t.Errorf("ssh-host 以外の at は形を問わない: %v", err)
	}
}
