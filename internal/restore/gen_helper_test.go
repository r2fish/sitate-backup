package restore

// U1e 自身の試験の準備: 偽の保管先に実際の世代(データのチャンク・エントリ・鍵表・マニフェスト)を置く。
// 期待値は元データから作り、置いたマニフェストの値を期待値にしない。

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

// specEntry は置きたいエントリ。Data は file・dump の元データ。
type specEntry struct {
	Type, T, Path, Target, Format string
	Data                          []byte
	Mode                          uint32
	Mtime                         int64
	// HashOf があれば、エントリの hash を Data でなくこの内容のハッシュにする(壊れの注入)。
	HashOf []byte
	// BadVersion が真なら ranges の版 ID を存在しないものにする(欠けの注入)。
	BadVersion bool
	NoAttrs    bool
}

type genWorld struct {
	t      *testing.T
	core   *fake.Core
	ws     *fake.ObjectStore
	Ref    string
	Snap   []byte
	Crypto fake.Crypto
}

const genRef = "m-0123456789abcdef0123456789abcdef"

func newGenWorld(t *testing.T) *genWorld {
	t.Helper()
	_, _, core := fake.NewTestStore(fake.RoleRead)
	return &genWorld{t: t, core: core, ws: &fake.ObjectStore{Core: core, Bucket: "b", Role: fake.RoleWrite, Params: contract.FixedRetention{}}, Ref: genRef, Snap: []byte("snap-key")}
}

func (g *genWorld) put(key, body string) string {
	g.t.Helper()
	pr, err := g.ws.Put(context.Background(), key, strings.NewReader(body), contract.RetentionDaily)
	if err != nil {
		g.t.Fatal(err)
	}
	return pr.VersionID
}

func (g *genWorld) hash(b []byte) string { return "blake3:" + hex.EncodeToString(g.Crypto.BLAKE3(b)) }

func (g *genWorld) chunk(plain []byte) (id, ver string) {
	id = fake.Codec{}.Hash(plain)
	return id, g.put("chunks/"+id[:2]+"/"+id, "fake:"+string(plain))
}

// build はエントリを保管先に置き、世代のマニフェストを書く。
func (g *genWorld) build(entries []specEntry) {
	g.t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	var lines, keys strings.Builder
	var total int64
	for i, e := range entries {
		m := map[string]any{"ref": "f:" + strings.Repeat(string(rune('a'+i)), 32), "t": e.T, "path": e.Path, "type": e.Type}
		if e.Type != "dump" { // U1c の dump のエントリは mode・uid・gid・mtime_ns を持たない(spec 4.4: dump は format・size・hash・ranges)
			m["mode"], m["uid"], m["gid"], m["mtime_ns"] = e.Mode, 1000, 1000, e.Mtime
		}
		if e.NoAttrs {
			delete(m, "mode")
		}
		switch e.Type {
		case "symlink":
			m["target"] = e.Target
		case "file", "dump":
			id, ver := g.chunk(e.Data)
			if e.BadVersion {
				ver = "no-such-version"
			}
			h := e.Data
			if e.HashOf != nil {
				h = e.HashOf
			}
			m["size"], m["hash"], m["ranges"] = len(e.Data), g.hash(h), [][]any{{id, ver, 0, len(e.Data)}}
			if e.Type == "dump" {
				m["format"] = e.Format
			}
			kb, _ := json.Marshal(map[string]string{"id": id, "v": ver, "k": key})
			keys.Write(append(kb, '\n'))
			total += int64(len(e.Data))
		}
		lb, _ := json.Marshal(m)
		lines.Write(append(lb, '\n'))
	}
	eid, ever := g.chunk([]byte(lines.String()))
	kid, kver := g.chunk([]byte(keys.String()))
	man, _ := json.Marshal(map[string]any{"format": "sitate.manifest/v1", "manifest_id": g.Ref, "health": "pass", "key_id": "k1",
		"targets":    []map[string]any{{"id": "t1", "status": "ok", "bytes": total}},
		"entries":    map[string]any{"chunks": []map[string]string{{"id": eid, "version_id": ever, "key": key}}},
		"chunk_keys": map[string]any{"chunks": []map[string]string{{"id": kid, "version_id": kver, "key": key}}}})
	g.put("manifests/"+g.Ref, "fake:"+string(man))
}

// restoreCall は (a) の入口の呼び出し。
func (g *genWorld) restoreCall(to string) contract.Call {
	var line contract.DestKeyLine
	line.SnapshotKey = base64.StdEncoding.EncodeToString(g.Snap)
	line.ReadKey.KeyID, line.ReadKey.Secret = "kR", "sR"
	b, _ := json.Marshal(line)
	return contract.Call{Words: []string{"restore"}, RunID: rtRun, SnapshotKeyIn: contract.NewSecret(b), Args: map[string][]string{
		contract.ArgBackup: {g.Ref}, contract.ArgTo: {to}, contract.ArgEndpoint: {"https://s3.example.invalid"}, contract.ArgBucket: {"b"}, contract.ArgPrefix: {"sitate/"}}}
}

func (g *genWorld) restorer() *Restore {
	rs := &fake.ObjectStore{Core: g.core, Bucket: "b", Role: fake.RoleRead, Params: contract.FixedRetention{}}
	return &Restore{Codec: fake.Codec{Size: 4}, KeyS: fake.KeyService{}, Crypto: fake.Crypto{}, Recorder: &fake.Recorder{}, Clock: &fake.Clock{T: fake.StartTime},
		OpenStore: func(context.Context, contract.StoreAddr, string, *contract.Secret) (contract.ObjectStore, error) {
			return rs, nil
		}}
}

// buildMulti は 1 つのファイルを parts の各チャンクにまたがらせて置く(エントリは 1 つ、ranges は parts の数)。
func (g *genWorld) buildMulti(path string, parts [][]byte) {
	g.t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	var rng [][]any
	var keys strings.Builder
	var all []byte
	for _, p := range parts {
		id, ver := g.chunk(p)
		rng = append(rng, []any{id, ver, 0, len(p)})
		kb, _ := json.Marshal(map[string]string{"id": id, "v": ver, "k": key})
		keys.Write(append(kb, '\n'))
		all = append(all, p...)
	}
	e, _ := json.Marshal(map[string]any{"ref": "f:" + strings.Repeat("a", 32), "t": "t1", "path": path, "type": "file", "mode": 0o644, "uid": 1, "gid": 1,
		"mtime_ns": mt, "size": len(all), "hash": g.hash(all), "ranges": rng})
	eid, ever := g.chunk(append(e, '\n'))
	kid, kver := g.chunk([]byte(keys.String()))
	man, _ := json.Marshal(map[string]any{"manifest_id": g.Ref, "health": "pass", "key_id": "k1",
		"entries":    map[string]any{"chunks": []map[string]string{{"id": eid, "version_id": ever, "key": key}}},
		"chunk_keys": map[string]any{"chunks": []map[string]string{{"id": kid, "version_id": kver, "key": key}}}})
	g.put("manifests/"+g.Ref, "fake:"+string(man))
}
