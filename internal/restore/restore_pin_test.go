package restore

import (
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// definition が無い・null のマニフェストは定義の写しの不一致(def_copy_ok: false、DEF_COPY_MISMATCH)。
func TestCheckDefCopyMissingIsMismatch(t *testing.T) {
	for _, def := range []string{"", "null"} {
		e := newEnv()
		ts := &rtState{r: e.Restore, snap: snapRec{Ref: refA}, man: &manifestDoc{DefHash: "sha256:" + hex64("1"), Definition: []byte(def)}}
		got := ts.checkDefCopy(ctx)
		if got == nil || *got || len(ts.warnings) != 1 || ts.warnings[0].Code != contract.CodeDefCopyMismatch {
			t.Errorf("definition=%q: %v %+v", def, got, ts.warnings)
		}
	}
}

// 固定した指紋が無い(登録の無い)復元先には、start を呼ばず SSH もせず、鍵を渡す前に止める。
func TestRestoreTestNoPinnedFingerprintStopsBeforeStart(t *testing.T) {
	w := newPass(t)
	delete(w.St.Items, "host_keys")
	_, err := w.RestoreTest(ctx, rtCall(false))
	harness.ExpectCode(t, err, contract.CodeRestoreEnrollMismatch)
	for _, op := range w.adapterOps() {
		if op == "start" {
			t.Error("固定した指紋が無いのに start を呼んだ")
		}
	}
	if len(w.SSH.Recorded()) != 0 || w.keyLine != "" {
		t.Error("SSH か鍵の受け渡しに進んだ")
	}
}
