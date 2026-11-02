package store

// Retention の試験の組み立て: 偽の保管先(制御できる時計)に、参照実装の Codec で作った本物の形のマニフェスト・鍵表・チャンクを置き、
// 状態ファイルの偽物に世代の一覧と記録した版を入れる。

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// snapKS はスナップショット鍵を返す KeyService(参照実装の Codec は 32 バイトの鍵を要る)。見出しから引いた鍵 ID が k1 であることを確かめる。
type snapKS struct {
	fake.KeyService
	t *testing.T
}

var testSnapKey = []byte("0123456789abcdef0123456789abcdef")

func (k snapKS) OpenSnapshotKey(_ context.Context, wrapped []byte, keyID, manifestID string) ([]byte, error) {
	if keyID != "k1" || len(wrapped) != 72 || !strings.HasPrefix(manifestID, "m-") {
		k.t.Errorf("OpenSnapshotKey の引数: keyID=%q wrapped=%d manifestID=%q", keyID, len(wrapped), manifestID)
	}
	return testSnapKey, nil
}

// simOff は共有の値の偽物: 模擬時間(clk)と実時刻の差を返す。
type simOff struct{ clk *fake.ManualClock }

func (o simOff) Offset() (time.Duration, error) { return o.clk.T.Sub(time.Now().UTC()), nil }

type fixture struct {
	t     *testing.T
	st    *fake.ObjectStore
	clk   *fake.ManualClock
	core  *fake.Core
	sf    *fake.StateFiles
	r     *Retention
	snaps []map[string]any
	objs  []map[string]any
	prot  []map[string]any
	n     int
	idx   *testIndex
	// ktParts は鍵表を行の途中で切る数(0・1 は 1 チャンク)。badLastKT なら最後のチャンクを開けない中身にする。
	ktParts   int
	badLastKT bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, clk, core := fake.NewTestStore(fake.RoleWrite)
	f := &fixture{t: t, st: st, clk: clk, core: core, sf: &fake.StateFiles{}}
	f.idx = &testIndex{}
	setItem(f.sf, "sealed", true)
	f.r = &Retention{Index: f.idx, Store: st, State: f.sf, Codec: &harness.RefCodec{}, KeyS: snapKS{t: t}, Clock: &fake.Clock{T: clk.T}, Offset: simOff{clk}}
	return f
}

func (f *fixture) put(key, body string) contract.PutResult {
	f.t.Helper()
	r, err := f.st.Put(ctx, key, strings.NewReader(body), contract.RetentionDaily)
	if err != nil {
		f.t.Fatal(err)
	}
	if strings.HasPrefix(key, "chunks/") {
		f.idx.add(contract.VersionRef{Key: key, VersionID: r.VersionID, Size: r.Size, ETag: r.ETag, RetainUntil: r.RetainUntil})
	}
	return r
}

func (f *fixture) chunkID() string { f.n++; return fmt.Sprintf("%064x", f.n) }

type snapshot struct {
	Ref      string
	Manifest objID
	// Refs はマニフェストが参照するエントリ・鍵表・データのチャンク。
	Refs []objID
}

// addSnapshot はデータのチャンク nData 個を持つ世代を作る。retentionUntil は世代の保持期限、protected なら保護中の世代にする。
func (f *fixture) addSnapshot(nData int, retentionUntil time.Time, protected bool, shared ...objID) snapshot {
	f.t.Helper()
	ref := fmt.Sprintf("m-%032x", f.n+1000)
	f.n++
	var s snapshot
	s.Ref = ref
	var rows []string
	for i := 0; i < nData; i++ {
		id := f.chunkID()
		r := f.put(chunkKey(id), "data-"+id)
		s.Refs = append(s.Refs, objID{chunkKey(id), r.VersionID})
		rows = append(rows, fmt.Sprintf(`{"id":%q,"v":%q,"k":"AAAA"}`, id, r.VersionID))
	}
	for _, sh := range shared { // ほかの世代と共有するデータのチャンク(重複排除)
		rows = append(rows, fmt.Sprintf(`{"id":%q,"v":%q,"k":"AAAA"}`, sh.Key[strings.LastIndex(sh.Key, "/")+1:], sh.VersionID))
	}
	ktRefs, ktObjs := f.keyTable(rows)
	s.Refs = append(s.Refs, ktObjs...)
	enID := f.chunkID()
	en := f.put(chunkKey(enID), "entries-"+enID)
	s.Refs = append(s.Refs, objID{chunkKey(enID), en.VersionID})
	key64 := base64.StdEncoding.EncodeToString(testChunkKey)
	body, _ := json.Marshal(map[string]any{
		"def_hash": "sha256:" + strings.Repeat("ab", 32), "binary_version": "0.1.0",
		"entries":    map[string]any{"chunks": []any{map[string]any{"id": enID, "version_id": en.VersionID, "key": key64}}},
		"chunk_keys": map[string]any{"chunks": ktRefs},
	})
	mark := binary.BigEndian.AppendUint32(nil, 1)
	obj, err := (&harness.RefCodec{}).Encode(contract.MagicManifest, strings.TrimPrefix(ref, "m-"), contract.Header{Kind: contract.MagicManifest, FormatVersion: contract.FormatVersion, Mark: mark, Wrapped: make([]byte, 72)}, body, testSnapKey)
	if err != nil {
		f.t.Fatal(err)
	}
	m := f.put("manifests/"+ref, string(obj))
	s.Manifest = objID{"manifests/" + ref, m.VersionID}
	f.snaps = append(f.snaps, map[string]any{
		"ref": ref, "version_id": m.VersionID, "size": m.Size, "etag": m.ETag, "retain_until": m.RetainUntil.Format(time.RFC3339),
		"created_at": f.clk.T.Format(time.RFC3339), "stored_at": f.clk.T.Format(time.RFC3339), "retention_class": "daily",
		"retention_until": retentionUntil.Format(time.RFC3339), "health": "pass", "targets_ok": true,
	})
	if protected {
		f.prot = append(f.prot, map[string]any{"ref": ref, "reasons": []string{"latest_healthy"}})
	}
	f.flush()
	return s
}

// addObject は共有物などの記録(objects[])を足す。
func (f *fixture) addObject(key string, r contract.PutResult, writtenAt time.Time) objID {
	f.objs = append(f.objs, objRec2(key, r, writtenAt))
	f.flush()
	return objID{key, r.VersionID}
}

func objRec2(key string, r contract.PutResult, at time.Time) map[string]any {
	return map[string]any{"key": key, "version_id": r.VersionID, "size": r.Size, "etag": r.ETag, "retain_until": r.RetainUntil.Format(time.RFC3339), "written_at": at.Format(time.RFC3339)}
}

func (f *fixture) flush() {
	setItem(f.sf, "snapshots", f.snaps)
	setItem(f.sf, "objects", f.objs)
	setItem(f.sf, "protected", f.prot)
}

// retainOf は偽の保管先が持つ今の retain-until。
func (f *fixture) retainOf(id objID) time.Time {
	f.t.Helper()
	vs, err := f.st.ListVersions(ctx, id.Key)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, v := range vs {
		if v.Key == id.Key && v.VersionID == id.VersionID {
			return v.RetainUntil
		}
	}
	f.t.Fatalf("版が無い: %+v", id)
	return time.Time{}
}

func (f *fixture) exists(id objID) bool {
	vs, _ := f.st.ListVersions(ctx, id.Key)
	for _, v := range vs {
		if v.Key == id.Key && v.VersionID == id.VersionID && !v.DeleteMarker {
			return true
		}
	}
	return false
}

func equalWithin(a, b time.Time, d time.Duration) bool { return a.Sub(b).Abs() <= d }

// recordAll は版の現在の保管先の retain-until を objects[] の記録にする(チャンクの記録は本来 index/chunks。試験では objects[] で代える)。
func (f *fixture) recordAll(ids []objID) {
	for _, id := range ids {
		if strings.HasPrefix(id.Key, "chunks/") {
			continue // チャンクの記録は put で Index に入っている
		}
		f.objs = append(f.objs, map[string]any{"key": id.Key, "version_id": id.VersionID, "size": 1, "etag": "e", "retain_until": f.retainOf(id).Format(time.RFC3339), "written_at": f.clk.T.Format(time.RFC3339)})
	}
	f.flush()
}

// put0 は t0 に書いた版として置き(時計を戻して書く)、保管先のロックを現在より先まで延ばす。
func (f *fixture) put0(key string) contract.PutResult {
	f.t.Helper()
	now := f.clk.T
	f.clk.T = now.Add(-100 * day)
	r := f.put(key, "alive")
	f.clk.T = now
	if err := f.core.PutRetention("b", key, r.VersionID, now.Add(50*day), fake.RoleWrite); err != nil {
		f.t.Fatal(err)
	}
	return r
}

// testIndex は contract.Index の最小の偽物(chunks/ の版の記録だけ。Records・SetRetain・Remove)。
type testIndex struct {
	recs    map[string]contract.VersionRef // 版 ID から
	removed []string
	setCnt  int
}

func (i *testIndex) add(x contract.VersionRef) {
	if i.recs == nil {
		i.recs = map[string]contract.VersionRef{}
	}
	i.recs[x.VersionID] = x
}

func (i *testIndex) Lookup(context.Context, string) (*contract.ChunkRef, error) { return nil, nil }
func (i *testIndex) Reserve(context.Context, contract.ChunkRef) error           { return nil }
func (i *testIndex) SetVersion(context.Context, string, string) error           { return nil }
func (i *testIndex) Rebuild(context.Context, [][]byte, []byte) error            { return nil }
func (i *testIndex) SetRetain(_ context.Context, v string, until time.Time) error {
	x := i.recs[v]
	x.RetainUntil = until
	i.recs[v] = x
	i.setCnt++
	return nil
}

func (i *testIndex) Records(context.Context) ([]contract.VersionRef, error) {
	var out []contract.VersionRef
	for _, x := range i.recs {
		out = append(out, x)
	}
	return out, nil
}

func (i *testIndex) Remove(_ context.Context, v string) error {
	delete(i.recs, v)
	i.removed = append(i.removed, v)
	return nil
}

var testChunkKey = []byte("fedcba9876543210fedcba9876543210")

// keyTable は鍵表(JSON Lines)を ktParts 個のチャンクに、行の途中で切って置く。
func (f *fixture) keyTable(rows []string) ([]any, []objID) {
	plain := []byte(strings.Join(rows, "\n") + "\n")
	parts := max(f.ktParts, 1)
	key64 := base64.StdEncoding.EncodeToString(testChunkKey)
	var refs []any
	var objs []objID
	for i := 0; i < parts; i++ {
		piece := plain[len(plain)*i/parts : len(plain)*(i+1)/parts]
		id := f.chunkID()
		sealed, err := (&harness.RefCodec{}).Seal(piece, testChunkKey, id)
		if err != nil {
			f.t.Fatal(err)
		}
		if f.badLastKT && i == parts-1 {
			sealed = []byte("garbage")
		}
		r := f.put(chunkKey(id), string(sealed))
		refs = append(refs, map[string]any{"id": id, "version_id": r.VersionID, "key": key64})
		objs = append(objs, objID{chunkKey(id), r.VersionID})
	}
	return refs, objs
}
