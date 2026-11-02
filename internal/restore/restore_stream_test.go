package restore

// ファイル全体をメモリに載せない書き出しと照合(チャンクごとに書き、照合は Codec.HashStream)。

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// countCodec は Hash(全体をメモリに載せる)と HashStream の呼び出しを数える。
type countCodec struct {
	fake.Codec
	hash, stream, maxHash int
}

func (c *countCodec) Hash(b []byte) string {
	c.hash++
	c.maxHash = max(c.maxHash, len(b))
	return c.Codec.Hash(b)
}
func (c *countCodec) HashStream(r io.Reader) (string, error) {
	c.stream++
	return c.Codec.HashStream(r)
}

// 複数チャンクにまたがる大きなファイルを復元して元データと一致させる。照合は HashStream(Hash に全体を渡さない)。
func TestRestoreLargeFileStreams(t *testing.T) {
	g := newGenWorld(t)
	parts := [][]byte{bytes.Repeat([]byte("A"), 1<<20), bytes.Repeat([]byte("B"), 1<<20), bytes.Repeat([]byte("C"), 1<<20), []byte("tail")}
	g.buildMulti("t1/big.bin", parts)
	r := g.restorer()
	cc := &countCodec{Codec: fake.Codec{Size: 4}}
	r.Codec = cc
	to := filepath.Join(t.TempDir(), "to")
	_, err := r.Restore(ctx, g.restoreCall(to))
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(to, "t1/big.bin"))
	if !bytes.Equal(got, bytes.Join(parts, nil)) {
		t.Errorf("内容が元データと違う: %d バイト", len(got))
	}
	if cc.stream != 1 {
		t.Errorf("照合が HashStream を使っていない: %d 回", cc.stream)
	}
	if cc.maxHash >= len(got) {
		t.Errorf("ファイル全体を Hash に渡した: %d", cc.maxHash)
	}
}

// 覚えておくチャンクは chunkCacheMax 個まで。
func TestChunkSourceCacheBounded(t *testing.T) {
	g := newGenWorld(t)
	g.buildMulti("t1/big.bin", [][]byte{[]byte("one"), []byte("two"), []byte("three"), []byte("four")})
	r := g.restorer()
	store, _ := r.OpenStore(ctx, contract.StoreAddr{}, "", nil)
	o := &objReader{store: store, codec: r.Codec, keyS: r.KeyS}
	vers, _ := store.ListVersions(ctx, "manifests/")
	m, _, err := o.manifest(ctx, g.Ref, vers[0].VersionID, g.Snap)
	if err != nil {
		t.Fatal(err)
	}
	ents, _ := o.loadEntries(ctx, m)
	rows, _ := o.keyTable(ctx, m)
	s := newChunkSource(ctx, o, rows)
	if err := s.content(ents[0], io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(s.cache) > chunkCacheMax {
		t.Errorf("キャッシュが上限 %d を超えた: %d", chunkCacheMax, len(s.cache))
	}
}
