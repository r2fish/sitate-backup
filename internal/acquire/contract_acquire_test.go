package acquire

// 契約試験(作業指示書 3.2): 索引の先行書き込みと中断・再実行のベクトル(spec 4.3「索引とチャンク鍵」・4.5)と、偽物を差し込んだ入口の外形。
// 骨格は NOT_IMPLEMENTED で落ち、nil を返すだけの実装は値の比較で落ち、仕様どおりの実装は panic しない。WP は骨格を実装し、この試験は変えない。
// ストリーム形式・file-retry・read-batch のベクトルは internal/contract の試験が参照実装(harness)で照らし、acquire が使う。

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var ctx = context.Background()

func newIndex(dir string) *Index {
	return &Index{Dir: dir, Codec: &harness.RefCodec{}, KeyS: fake.KeyService{}}
}

func ref(id string) contract.ChunkRef {
	return contract.ChunkRef{ChunkID: id, WrappedKey: bytes.Repeat([]byte{7}, 72), FirstSeen: "m-44444444444444444444444444444444", FormatVersion: contract.FormatVersion, Size: 11}
}

const idA = "3441db5e340ef39d1e2646d3f18face762e0f3ecba1a22577fcfe09499e506fe"
const idB = "4fa213fa1c1f5ae802e09c6e384b60390a0b5a1b0f228d77d89af00a84b29a21"

// 先行書き込み: 送る前に鍵を記録し(版 ID は空)、送信の応答の後に版 ID を追記する。
func TestContractIndexReserve(t *testing.T) {
	ix := newIndex(t.TempDir())
	got, err := ix.Lookup(ctx, idA)
	harness.Implemented(t, err)
	if err != nil || got != nil {
		t.Fatalf("未知のチャンクは (nil, nil): %v %v", got, err)
	}
	if err := ix.Reserve(ctx, ref(idA)); err != nil {
		t.Fatal(err)
	}
	got, err = ix.Lookup(ctx, idA)
	if err != nil || got == nil || got.VersionID != "" || !bytes.Equal(got.WrappedKey, ref(idA).WrappedKey) || got.FirstSeen != ref(idA).FirstSeen {
		t.Fatalf("先行書き込みの直後は鍵があり版 ID が無い: %+v %v", got, err)
	}
}

func TestContractIndexSetVersion(t *testing.T) {
	ix := newIndex(t.TempDir())
	err := ix.SetVersion(ctx, idA, "v1")
	harness.Implemented(t, err)
	if err == nil {
		t.Error("先行書き込みの無いチャンクへの版 ID の追記が成功した")
	}
	_ = ix.Reserve(ctx, ref(idA))
	if err := ix.SetVersion(ctx, idA, "v1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ix.Lookup(ctx, idA); got == nil || got.VersionID != "v1" {
		t.Errorf("版 ID: %+v", got)
	}
}

// 中断・再実行: 別のプロセス(新しい Index)でも、送った鍵と版 ID が残る。版 ID の無いチャンクは版 ID が空のまま(送り直す)。
func TestContractIndexResume(t *testing.T) {
	dir := t.TempDir()
	a := newIndex(dir)
	harness.Implemented(t, a.Reserve(ctx, ref(idA)))
	_ = a.SetVersion(ctx, idA, "v1")
	_ = a.Reserve(ctx, ref(idB)) // 送信の応答を受ける前に中断
	b := newIndex(dir)
	ga, _ := b.Lookup(ctx, idA)
	gb, _ := b.Lookup(ctx, idB)
	if ga == nil || ga.VersionID != "v1" || gb == nil || gb.VersionID != "" || !bytes.Equal(gb.WrappedKey, ref(idB).WrappedKey) {
		t.Fatalf("再実行後: %+v %+v", ga, gb)
	}
}

// 末尾の記録が長さに満たない(書きかけ)なら捨てる。それまでの記録は読める。
func TestContractIndexTornTail(t *testing.T) {
	dir := t.TempDir()
	a := newIndex(dir)
	harness.Implemented(t, a.Reserve(ctx, ref(idA)))
	p := filepath.Join(dir, "index", "chunks")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("索引は <Dir>/index/chunks にある: %v", err)
	}
	_, _ = f.Write(binary.BigEndian.AppendUint32(nil, 500))
	_, _ = f.Write([]byte("torn"))
	f.Close()
	if got, err := newIndex(dir).Lookup(ctx, idA); err != nil || got == nil {
		t.Errorf("書きかけの末尾を捨てて残りを読む: %+v %v", got, err)
	}
}

func TestContractIndexRemove(t *testing.T) {
	ix := newIndex(t.TempDir())
	harness.Implemented(t, ix.Reserve(ctx, ref(idA)))
	_ = ix.SetVersion(ctx, idA, "v1")
	if got, _ := ix.Lookup(ctx, idA); got == nil || got.VersionID != "v1" {
		t.Fatalf("Remove の前提: %+v", got)
	}
	if err := ix.Remove(ctx, "v1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := newIndex(ix.Dir).Lookup(ctx, idA); got != nil && got.VersionID == "v1" {
		t.Errorf("prune が消した版は索引から外す: %+v", got)
	}
}

func TestContractIndexRebuild(t *testing.T) {
	err := newIndex(t.TempDir()).Rebuild(ctx, [][]byte{[]byte("garbage")}, nil)
	harness.Implemented(t, err)
	if err == nil {
		t.Error("読めないマニフェストからの再構成が成功した")
	}
}

func shapeAcquire() *Acquire {
	store, _, _ := fake.NewTestStore(fake.RoleWrite)
	return &Acquire{Store: store, State: &fake.StateFiles{}, Index: &fake.Index{M: map[string]contract.ChunkRef{}}, Codec: fake.Codec{Size: 4}, KeyS: fake.KeyService{}, Adapters: fake.NewAdapterRunner(), Clock: &fake.Clock{T: fake.StartTime}}
}

func shapeCall(t *testing.T, words []string, f func(*Acquire, context.Context, contract.Call) (contract.Reply, error)) {
	t.Helper()
	r, err := f(shapeAcquire(), ctx, contract.Call{Words: words})
	harness.ShapeOrError(t, words, r, err)
}

func TestContractAcquireSelftest(t *testing.T) {
	r, err := shapeAcquire().Selftest(ctx)
	harness.ShapeOrError(t, []string{"backup"}, r, err)
}

func TestContractAcquireRun(t *testing.T) { shapeCall(t, []string{"backup"}, (*Acquire).Run) }

// init の前(state.json の initialized が偽)の backup は NOT_INITIALIZED(spec 3.1.3)。常に INTERNAL を返す実装を落とす最小の 1 件
// (取得の本体の検証 = ストリーム形式・file-retry・read-batch は U1c の試験に回す。設計の監督の了承)。
func TestContractAcquireRunNotInitialized(t *testing.T) {
	a := shapeAcquire()
	a.State = &fake.StateFiles{Items: map[string]json.RawMessage{"initialized": json.RawMessage("false")}}
	_, err := a.Run(ctx, contract.Call{Words: []string{"backup"}, RunID: testRunID})
	harness.ExpectCode(t, err, contract.CodeNotInitialized)
}

// 取り消された文脈を受けた実行は、保管先と索引へ書かずに RUN_ABORTED(spec 3.1.1 の規則 11)。
func TestContractAcquireRunAborted(t *testing.T) {
	a, st := newSrcAcquire(map[string]json.RawMessage{"sealed": sealedItem(true)})
	ad, ix := a.Adapters.(*fake.AdapterRunner), a.Index.(*fake.Index)
	c, cancel := context.WithCancel(ctx)
	cancel()
	_, err := a.Run(c, contract.Call{Words: []string{"backup"}, RunID: testRunID})
	harness.ExpectCode(t, err, contract.CodeRunAborted)
	if v, _ := a.Store.ListVersions(ctx, ""); len(v) != 0 {
		t.Errorf("取り消された実行が保管先へ書いた: %v", v)
	}
	if len(ad.Calls) != 0 || len(ix.M) != 0 || len(st.Ledger) != 0 {
		t.Errorf("取り消された実行が取得・索引・台帳へ進んだ: アダプタ %d 回、索引 %d 件、台帳 %d 件", len(ad.Calls), len(ix.M), len(st.Ledger))
	}
}

func TestContractAcquireSourceCheck(t *testing.T) {
	shapeCall(t, []string{"source", "check"}, (*Acquire).SourceCheck)
}

func TestContractSurveyorSurvey(t *testing.T) {
	_, err := (&Surveyor{}).Survey(ctx, "nosuchscope")
	harness.Implemented(t, err)
	if err == nil {
		t.Error("範囲 full / db_only 以外の調査が成功した")
	}
}

func TestContractSurveyorJudge(t *testing.T) {
	_, err := (&Surveyor{}).Judge(ctx, []byte("not json"), nil)
	harness.Implemented(t, err)
	if err == nil {
		t.Error("読めない調査の値の判定が成功した")
	}
}

func TestContractRunCleanup(t *testing.T) {
	c, u, err := (&RunCleanup{}).Cleanup(ctx, "r-1", "r-0")
	harness.Implemented(t, err)
	if err != nil || c == nil || u == nil {
		t.Errorf("片付ける資源が無くても一覧は配列: %v %v %v", c, u, err)
	}
}

func TestContractSourceEnroller(t *testing.T) {
	_, err := (&SourceEnroller{}).Enroll(ctx, contract.EnrollInput{Kind: "nosuch"})
	harness.Implemented(t, err)
	if err == nil {
		t.Error("kind が source でない登録が成功した")
	}
}
