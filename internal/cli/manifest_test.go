package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

type snapKeyService struct{ fake.KeyService }

func (snapKeyService) NewSnapshotKey(context.Context, string) ([]byte, []byte, string, error) {
	return bytes.Repeat([]byte{7}, 32), bytes.Repeat([]byte{9}, 72), "k1", nil
}

func ckey() string { return strings.Repeat("A", 43) + "=" }

func material(t *testing.T, status string) ManifestMaterial {
	t.Helper()
	d, _ := defDeps(t, "")
	norm, _, issues, err := d.State.ValidateDef(context.Background(), []byte(defYAML), false)
	if err != nil || norm == nil {
		t.Fatalf("%v %v", err, issues)
	}
	ref := ChunkRefMaterial{ID: strings.Repeat("a", 64), VersionID: "v1", Key: ckey()}
	return ManifestMaterial{
		ManifestID: "m-" + strings.Repeat("1", 32), RepoID: "rp-" + strings.Repeat("2", 32), CreatedAt: time.Date(2026, 11, 2, 3, 4, 5, 0, time.UTC),
		DefHash: "sha256:" + strings.Repeat("3", 64), BinaryVersion: "0.1.0", Definition: norm,
		Targets:      []TargetMaterial{{ID: "t1", Kind: "files", Status: status, Files: 3, Bytes: 10, ChangedDuringRead: 1}},
		EntriesCount: 3, EntriesChunks: []ChunkRefMaterial{ref}, KeyTableCount: 1, KeyTableChunks: []ChunkRefMaterial{ref}, ChunksReferenced: 2,
	}
}

// マニフェストの本体はスキーマに合い、チェックの無い世代は checks が空で、全対象 ok なら pass。
func TestManifestBodyMatchesSchema(t *testing.T) {
	body, health, err := buildManifestBody(material(t, "ok"), "k1", contract.RetentionMonthly)
	if err != nil {
		t.Fatal(err)
	}
	if e := contract.Validate("data/manifest.json", body); e != nil {
		t.Fatalf("%v\n%s", e, body)
	}
	if health != "pass" || !strings.Contains(string(body), `"checks":[]`) || !strings.Contains(string(body), `"parent":null`) {
		t.Errorf("health=%s %s", health, body)
	}
	m := material(t, "partial")
	if _, h, _ := buildManifestBody(m, "k1", contract.RetentionDaily); h != "incomplete" {
		t.Errorf("partial の世代の健常性 %s", h)
	}
	m.Targets[0].Status = "ok"
	m.ChunkKeysMissing = 1
	if _, h, _ := buildManifestBody(m, "k1", contract.RetentionDaily); h != "incomplete" {
		t.Errorf("鍵の無いチャンクがある世代の健常性 %s", h)
	}
	if allTargetsFailed(material(t, "ok").Targets) || !allTargetsFailed(material(t, "failed").Targets) {
		t.Error("allTargetsFailed")
	}
}

// 書き込み: manifests/<id> に保持区分つきで置かれ、復号すると本体に戻る(見出しの印は鍵 ID の 4 バイト)。
func TestManifestWriterRoundTrip(t *testing.T) {
	store, _, _ := fake.NewTestStore(fake.RoleWrite)
	w := ManifestWriter{Codec: &harness.RefCodec{}, KeyS: snapKeyService{}, Store: store}
	m := material(t, "ok")
	got, err := w.Write(context.Background(), m, contract.RetentionMonthly)
	if err != nil || got.Health != "pass" || got.Class != contract.RetentionMonthly || got.Put.VersionID == "" {
		t.Fatalf("%+v %v", got, err)
	}
	rc, err := store.Get(context.Background(), "manifests/"+m.ManifestID, "")
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := io.ReadAll(rc)
	plain, err := (&harness.RefCodec{}).Decode(contract.MagicManifest, strings.TrimPrefix(m.ManifestID, "m-"), sealed, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		RetentionClass string `json:"retention_class"`
	}
	_ = json.Unmarshal(plain, &back)
	if back.RetentionClass != "monthly" {
		t.Errorf("保持区分 %q", back.RetentionClass)
	}
	if contract.Validate("data/manifest.json", plain) != nil {
		t.Errorf("復号した本体がスキーマに合わない")
	}
	if mk, _ := keyIDMark("k12"); !bytes.Equal(mk, []byte{0, 0, 0, 12}) {
		t.Errorf("印 %v", mk)
	}
}
