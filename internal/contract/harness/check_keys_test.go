package harness

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// params・metrics・finding_codes の照合(作業指示書 3.2)。正しい 3 つ組は食い違い 0 件で、キーが外れた要求・結果は検出される。
func TestAdapterCheckKeys(t *testing.T) {
	read := func(n string) []byte {
		b, err := os.ReadFile(filepath.Join(VectorPath("adapter/valid"), n+".json"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	decl, req, res := read("decl-check"), read("check-request"), read("check-result")
	if bad := CheckKeys(decl, req, res); len(bad) != 0 {
		t.Fatalf("正しい 3 つ組が食い違う: %v", bad)
	}
	rep := func(b []byte, a, c string) []byte {
		if !bytes.Contains(b, []byte(a)) {
			t.Fatalf("置換元 %q が無い", a)
		}
		return bytes.Replace(b, []byte(a), []byte(c), 1)
	}
	for name, c := range map[string][3][]byte{
		"要求に宣言されていない param":  {decl, rep(req, `"ratio_warn"`, `"ratio_other"`), res},
		"宣言された param が要求に無い": {rep(decl, `"ratio_warn"`, `"ratio_warn","extra_param"`), req, res},
		"宣言されていない metric":    {decl, req, rep(res, `"ratio"`, `"other"`)},
		"宣言されていない finding":   {decl, req, rep(res, `"HIGH_ENTROPY_RATIO"`, `"OTHER_CODE"`)},
	} {
		if len(CheckKeys(c[0], c[1], c[2])) == 0 {
			t.Errorf("%s を検出しない", name)
		}
	}
}
