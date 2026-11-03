package restore

// gate を通る --to の字面(restore-dest: / source: / ssh://)は、イテレーション 1 の核では宛先として扱えない(spec 3.1.2.4。
// 復元先の宛先はイテレーション 2)。普通のパスとして読んで cwd の相対の場所へ書いてはならない(まとめレビュー C1)。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func TestRestoreToMustBeAbsoluteLocalDir(t *testing.T) {
	for _, to := range []string{"ssh://../keys/pending", "source:../out", "restore-dest:x", "rel/dir", "../up", "."} {
		t.Run(to, func(t *testing.T) {
			g := newGenWorld(t)
			g.build([]specEntry{
				{Type: "file", T: "pending.json", Path: "pending.json", Data: []byte(`{"op":"key-rotate"}`), Mode: 0o600, Mtime: mt},
				{Type: "file", T: "keyring", Path: "keyring", Data: []byte("x"), Mode: 0o600, Mtime: mt},
			})
			base := t.TempDir()
			state := filepath.Join(base, "state")
			if err := os.MkdirAll(filepath.Join(state, "keys"), 0o700); err != nil {
				t.Fatal(err)
			}
			old, _ := os.Getwd()
			defer os.Chdir(old)
			if err := os.Chdir(state); err != nil {
				t.Fatal(err)
			}
			r := g.restorer()
			opened := false
			inner := r.OpenStore
			r.OpenStore = func(c context.Context, a contract.StoreAddr, k string, s *contract.Secret) (contract.ObjectStore, error) {
				opened = true
				return inner(c, a, k, s)
			}
			_, err := r.Restore(ctx, g.restoreCall(to))
			ce := harness.ExpectCode(t, err, contract.CodeArgInvalid)
			if string(ce.Detail) != `{"flag":"--to","reason":"format"}` || opened {
				t.Errorf("detail=%s opened=%v", ce.Detail, opened)
			}
			var found []string
			_ = filepath.Walk(base, func(p string, fi os.FileInfo, _ error) error {
				if !fi.IsDir() {
					found = append(found, p)
				}
				return nil
			})
			if len(found) != 0 {
				t.Errorf("何かが作られた: %v", found)
			}
		})
	}
}

// H3: restore-test --dry-run は実行しない(spec 3.1.1)。復元先も保管先の読み出しも実行の記録も動かさない。
func TestRestoreTestDryRunRunsNothing(t *testing.T) {
	w := newPassOpts(t, worldOpts{})
	c := rtCall(false)
	c.DryRun = true
	_, err := w.RestoreTest(ctx, c)
	var ni *contract.NotImplemented
	if !errors.As(err, &ni) {
		t.Fatalf("NOT_IMPLEMENTED を期待したが %v", err)
	}
	if n := len(w.SSH.Recorded()); n != 0 {
		t.Errorf("SSH %d 回", n)
	}
	if n := len(w.Rec.Entries); n != 0 {
		t.Errorf("実行の記録 %d 件", n)
	}
}

// U1: 復元先が 0 件を報告したら、マニフェストも 0 件でも合格にしない(spec 3.5 の「照合の場所」: 0 件の報告で合格にしない)。
func TestCheckAgainstManifestRejectsZeroReport(t *testing.T) {
	for name, man := range map[string]string{"期待も報告も 0 件": `{"targets":[]}`, "期待 0 件の対象が ok": `{"targets":[{"id":"t1","status":"ok","files":0,"bytes":0,"dumps":[]}]}`, "期待 2 件・報告 0 件": `{"targets":[{"id":"t1","status":"ok","files":2,"bytes":10}]}`} {
		m := &manifestDoc{}
		if err := json.Unmarshal([]byte(man), m); err != nil {
			t.Fatal(err)
		}
		st := &rtState{man: m, remote: remoteOut{Files: 0, Bytes: 0}}
		if err := st.checkAgainstManifest(); err == nil {
			t.Errorf("%s: 0 件の報告が合格した", name)
		} else {
			harness.ExpectCode(t, err, contract.CodeRestoreMismatch)
		}
	}
}

// flakyStore は指定のキーの Get を失敗させる(一時的な読み出しの失敗)。
type flakyStore struct {
	contract.ObjectStore
	key string
	err error
}

func (f flakyStore) Get(ctx context.Context, key, version string) (io.ReadCloser, error) {
	if key == f.key {
		return nil, f.err
	}
	return f.ObjectStore.Get(ctx, key, version)
}

// U2: latest の解決で、新しい世代のマニフェストの一時的な読み出し失敗(到達できない)を、黙って古い世代にせず誤りで止める。
// 復号できない版(偽の版)だけを飛ばす(spec 4.2)。
func TestResolveLatestStopsOnTransientReadFailure(t *testing.T) {
	g := newGenWorld(t)
	older, newer := "m-"+strings.Repeat("b", 32), "m-"+strings.Repeat("a", 32) // 時刻が同じでも一覧の並びで newer が先
	g.Ref = older
	g.build(goodEntries())
	g.Ref = newer
	g.build(goodEntries())
	to := t.TempDir()
	call := g.restoreCall(to)
	call.Args[contract.ArgBackup] = []string{"latest"}
	r := g.restorer()
	inner := r.OpenStore
	r.OpenStore = func(c context.Context, a contract.StoreAddr, k string, s *contract.Secret) (contract.ObjectStore, error) {
		st, err := inner(c, a, k, s)
		return flakyStore{ObjectStore: st, key: "manifests/" + newer, err: newErr(contract.CodeStorageUnreachable, map[string]any{"target": "storage", "reason": "timeout", "http_status": nil})}, err
	}
	_, err := r.Restore(ctx, call)
	harness.ExpectCode(t, err, contract.CodeStorageUnreachable)
	if des, _ := os.ReadDir(to); len(des) != 0 {
		t.Errorf("古い世代を復元した: %v", des)
	}
}
