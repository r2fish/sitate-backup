package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// VectorPath は internal/contract/testdata/<rel> の絶対パス(このソースの位置から求める。試験の作業ディレクトリによらない)。
func VectorPath(rel string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata", rel)
}

// LoadVectors は試験ベクトルの JSON を into へ読む。読めなければ失敗させる。
func LoadVectors(t testing.TB, rel string, into any) {
	t.Helper()
	b, err := os.ReadFile(VectorPath(rel))
	if err != nil {
		t.Fatalf("試験ベクトルを読めない: %v", err)
	}
	if err := json.Unmarshal(b, into); err != nil {
		t.Fatalf("試験ベクトル %s: %v", rel, err)
	}
}

// Catch は fn を走らせ、骨格が NOT_IMPLEMENTED で panic したとき(error を返さない関数の骨格)を
// 「未実装」の印つきの失敗にする。それ以外の panic は再送出する(実装の誤りとして落ちる)。
func Catch(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			if ni, ok := r.(*contract.NotImplemented); ok {
				t.Fatalf("%s %s", NotImplementedMarker, ni.Feature)
			}
			panic(r)
		}
	}()
	fn()
}

// FailNil は「何も返さずに nil の誤りで終わる」実装を落とすための検査。値が空なら失敗させる。
func FailNil(t *testing.T, what string, ok bool) {
	t.Helper()
	if !ok {
		t.Fatalf("%s: 契約の値を返していない", what)
	}
}
