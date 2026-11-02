package cli

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/state"
)

// H6: config/<def_hash> は SITG(用途別の鍵 config)で保管先へ書き、版を objects[] に記録する。
func TestApplyDefineWritesSealedConfigAndRecordsVersion(t *testing.T) {
	a := &applyStub{}
	f, _ := applyEnv(t, a)
	st, _, _ := fake.NewTestStore(fake.RoleWrite)
	f.d.Object, f.d.Codec, f.d.KeyS = st, f.rec.Codec, fake.KeyService{}
	a.changes = []contract.ChangeOps{sealChange(applyDef)}
	if _, err := runApply(t, f, contract.Call{RunID: runA}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	norm, hash, err := f.d.State.ReadDef(ctx)
	if err != nil {
		t.Fatal(err)
	}
	objs, err := state.ReadObjects(ctx, f.d.State)
	if err != nil {
		t.Fatal(err)
	}
	var cfg *state.ObjectRec
	for i := range objs {
		if objs[i].Key == "config/"+hash {
			cfg = &objs[i]
		}
	}
	if cfg == nil || cfg.VersionID == "" {
		t.Fatalf("objects[] に config の版が無い: %+v", objs)
	}
	rc, err := st.Get(ctx, cfg.Key, cfg.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := io.ReadAll(rc)
	if strings.Contains(string(sealed), "app01.example.internal") {
		t.Fatal("定義が平文で保管先に残った")
	}
	key, _ := fake.KeyService{}.DerivedKey("config")
	plain, err := f.rec.Codec.Decode(contract.MagicConfig, strings.TrimPrefix(hash, contract.HashPrefix), sealed, key)
	if err != nil || !strings.Contains(string(plain), "app01.example.internal") || !strings.Contains(string(plain), string("\"normalized\"")) {
		t.Fatalf("SITG として開けない: %v %s", err, plain)
	}
	_ = norm
}
