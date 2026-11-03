package approval

// D-0160(spec 3.1.2.9 の seal): propose seal は未解決の参照名を許さない。seal の前の def validate が pending_refs[] で通した storage[0].key_ref・restore.dest.key_ref も、
// 提案の時点で解決していなければ提案を作らない(DEF_INVALID、reason: unresolved_ref)。解決していれば作る。実物の StateFiles(参照名の解決つき)で確かめる。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/state"
)

const sealDef = `version: 1
source: {host: app01.example.internal, targets: [{id: t1, files: /srv/app/data}]}
storage: [{endpoint: "https://s3.example-provider.com", bucket: b, key_ref: storage}]
restore: {dest: {at: "ssh://restore@203.0.113.10:22", key_ref: restore-init}}
notify: {to: [ops@example.jp]}
`

// destHost が空でなければ、そのホストで手順 9 の登録の記録(host_keys[] と restore_dest_enrollment)を置く。
func sealEnv(t *testing.T, destHost string, have ...string) *env {
	e := newEnv(t, true)
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	sf := &state.StateFiles{Dir: t.TempDir(), Clock: e.clock, Refs: func(_ context.Context, r string) (bool, error) { return set[r], nil }}
	norm, _, _, err := sf.ValidateDef(ctx, []byte(sealDef), true)
	if err != nil || norm == nil {
		t.Fatalf("定義を整えられない: %v", err)
	}
	if err := sf.StageDef(ctx, norm); err != nil {
		t.Fatal(err)
	}
	if destHost != "" {
		h, err := sf.Lock(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_ = sf.Update(ctx, h, "host_keys", json.RawMessage(`[{"role":"restore_dest","host":"`+destHost+`","host_key_fp":"SHA256:x","session_id":null}]`))
		_ = sf.Update(ctx, h, "restore_dest_enrollment", json.RawMessage(`{"egress":["192.0.2.1"]}`))
		h.Release()
	}
	e.st.Items["sealed"] = json.RawMessage(`false`)
	e.a.State = &sealState{StateFiles: e.st, real: sf}
	e.a.ReadDefinition = func(context.Context, string) ([]byte, error) { return []byte(sealDef), nil }
	return e
}

// sealState は参照名の解決と候補を実物の StateFiles に任せ、残りは偽の StateFiles で受ける。
type sealState struct {
	*fake.StateFiles
	real *state.StateFiles
}

func (s *sealState) ValidateDef(c context.Context, d []byte, skip bool) ([]byte, string, json.RawMessage, error) {
	return s.real.ValidateDef(c, d, skip)
}
func (s *sealState) ReadCandidate(c context.Context) ([]byte, string, bool, error) {
	return s.real.ReadCandidate(c)
}

func TestProposeSealRefusesUnresolvedRefs(t *testing.T) {
	for name, e := range map[string]*env{
		"storage の秘密が無い":     sealEnv(t, "203.0.113.10"),
		"登録の記録が無い(ssh-host)": sealEnv(t, "", "storage"),
		"別のホストの記録では解決にしない":   sealEnv(t, "198.51.100.9", "storage"),
	} {
		err := proposeErr(e, "seal", map[string][]string{"file": {"-"}})
		ce, ok := err.(*contract.Error)
		if !ok || ce.Code != contract.CodeDefInvalid || !strings.Contains(string(ce.Detail), "unresolved_ref") {
			t.Fatalf("%s: DEF_INVALID(unresolved_ref) のはず: %v", name, err)
		}
	}
}

func TestProposeSealAcceptsResolvedRefs(t *testing.T) {
	e := sealEnv(t, "203.0.113.10", "storage") // ssh-host の restore.dest.key_ref は秘密ファイルでなく登録の記録で解決する(D-0162)
	if err := proposeErr(e, "seal", map[string][]string{"file": {"-"}}); err != nil {
		t.Fatalf("参照名がそろえば提案を作る: %v", err)
	}
}
