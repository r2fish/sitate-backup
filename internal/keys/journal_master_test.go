package keys

// 主経路の第 2 段で見つけた誤り E3 の試験: 実物の Recorder と実物の KeyService を、実際のディレクトリの上で結ぶ
// (契約試験は偽の KeyService を使うため、master.json が確定の最後まで無いという実物の配置を通らない)。
//   - マスター鍵が keys/pending/master.json にだけある間(init の後、phrase verify の確定の前)も、記録は journal 鍵を得て書ける。
//     この間は保管先へ書かず(確定を捨てて作り直すとき、保管先に復号できない項目を残さないため)、確定の後に SyncRemote が写す。
//   - マスター鍵がまだ 1 つも無い間(init の前の init-storage)の write_intent は、落とさずに平文でない形では書けないため、
//     鍵のない間は journal/pre-master に置き、鍵ができた最初の追記の前に項目として移す。
//   - init を確定の前に再実行して保留を捨てるとき、捨てるマスター鍵で封じた journal の項目も捨てる。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
	"github.com/r2fish/sitate-backup/internal/state"
)

type jenv struct {
	dir   string
	ks    *KeyService
	rec   *state.Recorder
	store *fake.ObjectStore
}

func newJenv(t *testing.T) *jenv {
	t.Helper()
	dir := t.TempDir()
	codec := &harness.RefCodec{}
	ks := &KeyService{Dir: dir, Codec: codec, Crypto: &harness.RefCrypto{}}
	clk := &fake.Clock{T: fake.StartTime}
	sf := &state.StateFiles{Dir: dir, Codec: codec, KeyS: ks, Clock: clk}
	store, _, _ := fake.NewTestStore(fake.RoleWrite)
	return &jenv{dir: dir, ks: ks, store: store, rec: &state.Recorder{State: sf, Store: store, Codec: codec, KeyS: ks, Notifier: &fake.Notifier{}, Clock: clk, Dir: dir}}
}

func (e *jenv) putMaster(t *testing.T, rel string) {
	t.Helper()
	p := filepath.Join(e.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"current":"k1","keys":[{"key_id":"k1","key":"`+phraseMasterKey+`"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func intent(key string) []byte {
	b, _ := json.Marshal(map[string]any{"at": "2026-11-03T00:00:00Z", "kind": "write_intent", "run_id": "r-x", "body": map[string]any{"keys": []string{key}}})
	return b
}

func remoteJournal(e *jenv) int {
	vs, _ := e.store.ListVersions(ctx, "journal/")
	return len(vs)
}

// 保留の間(master.json が無く keys/pending/master.json がある)も記録でき、保管先へは書かず、確定の後に写す。同じ鍵で読める。
func TestJournalRecordsWhileMasterIsPending(t *testing.T) {
	e := newJenv(t)
	e.putMaster(t, contract.KeysPendingDir+"/"+contract.KeysPendingMaster)
	if _, err := e.rec.Record(ctx, intent("repo.json")); err != nil {
		t.Fatalf("保留の間の write_intent を記録できない: %v", err)
	}
	if n := remoteJournal(e); n != 0 {
		t.Fatalf("確定の前に保管先へ journal を書いた: %d 件", n)
	}
	if err := os.Rename(filepath.Join(e.dir, contract.KeysPendingDir, contract.KeysPendingMaster), filepath.Join(e.dir, contract.KeysMasterFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.rec.Record(ctx, intent("keyring/x")); err != nil {
		t.Fatalf("確定の後の記録: %v", err)
	}
	if err := e.rec.SyncRemote(ctx); err != nil {
		t.Fatal(err)
	}
	if n := remoteJournal(e); n != 2 {
		t.Fatalf("確定の後の SyncRemote で 2 件が保管先に写るはず: %d", n)
	}
	items, err := e.rec.Entries(ctx)
	if err != nil || len(items) != 2 || items[0].Seq != 1 || items[1].Seq != 2 {
		t.Fatalf("確定の前後で同じ鍵で読めるはず: %v %v", items, err)
	}
}

// マスター鍵がまだ無い間の write_intent は落とさず、鍵ができた最初の追記の前に連番の項目として移る。平文は journal/ の項目名の形で置かない。
func TestJournalKeepsIntentBeforeAnyMaster(t *testing.T) {
	e := newJenv(t)
	if _, err := e.rec.Record(ctx, intent("probe/aa")); err != nil {
		t.Fatalf("マスター鍵が無い間の write_intent が失敗した: %v", err)
	}
	es, _ := os.ReadDir(filepath.Join(e.dir, "journal"))
	for _, f := range es {
		if strings.HasPrefix(f.Name(), "0000") {
			t.Fatalf("鍵が無いのに封じた項目ができた: %s", f.Name())
		}
	}
	e.putMaster(t, contract.KeysPendingDir+"/"+contract.KeysPendingMaster)
	if _, err := e.rec.Record(ctx, intent("repo.json")); err != nil {
		t.Fatal(err)
	}
	items, err := e.rec.Entries(ctx)
	if err != nil || len(items) != 2 {
		t.Fatalf("2 件になるはず: %v %v", items, err)
	}
	if !strings.Contains(string(items[0].Body), "probe/aa") || !strings.Contains(string(items[1].Body), "repo.json") {
		t.Fatalf("鍵が無かった間の write_intent が先頭に移っていない: %s / %s", items[0].Body, items[1].Body)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "journal", "pre-master")); err == nil {
		t.Fatal("移した後に退避が残っている")
	}
}

// init を確定の前に再実行して保留を捨てるとき、捨てるマスター鍵で封じた journal の項目も捨てる(新しい鍵では読めないため)。
func TestKeysInitRerunDropsJournalSealedByDiscardedMaster(t *testing.T) {
	e, call := initPhraseEnv()
	useBundle(e, layout())
	if _, err := e.k.Init(ctx, call); err != nil {
		t.Fatal(err)
	}
	jd := filepath.Join(e.dir, "journal")
	item := filepath.Join(jd, "000000000001-"+strings.Repeat("a", 64))
	if err := os.MkdirAll(jd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(item, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.k.Init(ctx, call); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(item); err == nil {
		t.Fatal("捨てた保留のマスター鍵で封じた項目が残っている")
	}
}

// F5・E3 の追認の条件: 確定の前に init を再実行しても journal の連鎖が切れず読め、保管先に実在する版を指す write_intent は失われない。
// 実物の KeyService・Recorder・Keys.Init を同じ状態ディレクトリの上で結ぶ。再実行でマスター鍵が変わるので、古い鍵で封じた項目は捨て、write_intent だけを戻す。
func TestKeysInitRerunKeepsWriteIntentsAndJournalReadable(t *testing.T) {
	e, call := initPhraseEnv()
	useBundle(e, layout())
	codec := &harness.RefCodec{}
	ks := &KeyService{Dir: e.dir, Codec: codec, Crypto: &harness.RefCrypto{}}
	clk := &fake.Clock{T: fake.StartTime}
	store, _, _ := fake.NewTestStore(fake.RoleWrite)
	rec := &state.Recorder{State: &state.StateFiles{Dir: e.dir, Codec: codec, KeyS: ks, Clock: clk}, Store: store, Codec: codec, KeyS: ks, Notifier: &fake.Notifier{}, Clock: clk, Dir: e.dir}
	e.k.KeyS, e.k.Recorder, e.k.Codec = ks, rec, codec
	if _, err := rec.Record(ctx, intent("probe/before-init")); err != nil { // init の前(マスター鍵が無い間)
		t.Fatal(err)
	}
	if _, err := e.k.Init(ctx, call); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Record(ctx, intent("repo.json")); err != nil { // 古い保留の鍵で封じる
		t.Fatal(err)
	}
	endB, _ := json.Marshal(map[string]any{"at": "2026-11-03T00:00:00Z", "kind": "run_end", "run_id": "r-x", "body": map[string]any{}})
	if _, err := rec.Record(ctx, endB); err != nil {
		t.Fatal(err)
	}
	if _, err := e.k.Init(ctx, call); err != nil { // 確定の前の再実行: 鍵が変わる
		t.Fatal(err)
	}
	if _, err := rec.Record(ctx, intent("keyring/x")); err != nil {
		t.Fatalf("再実行の後に記録できない: %v", err)
	}
	items, err := rec.Entries(ctx)
	if err != nil {
		t.Fatalf("再実行の後に journal を読めない(連鎖が切れた): %v", err)
	}
	var got []string
	for i, it := range items {
		if int(it.Seq) != i+1 {
			t.Errorf("連番が 1 から続かない: %v", items)
		}
		got = append(got, it.Kind+":"+string(it.Body))
	}
	want := []string{`write_intent:{"keys":["probe/before-init"]}`, `write_intent:{"keys":["repo.json"]}`, `write_intent:{"keys":["keyring/x"]}`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("write_intent だけが連番の順に残るはず: %v", got)
	}
}

// F9: pending への切り替えは keys/master.json が無いときだけ。master.json が壊れているときに保留の新しい鍵(key-rotate など)で封じない。
func TestJournalKeyDoesNotFallBackWhenMasterIsCorrupt(t *testing.T) {
	e := newJenv(t)
	e.putMaster(t, contract.KeysPendingDir+"/"+contract.KeysPendingMaster)
	if err := os.WriteFile(filepath.Join(e.dir, contract.KeysMasterFile), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ks.DerivedKey("journal"); err == nil {
		t.Fatal("master.json が壊れているのに保留の鍵で journal 鍵を導いた")
	}
}
