package contract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// strayContractTests は、TestContract で始まらない Test 関数の名前を返す。make test-contract と make contract-status は
// 名前で契約試験を選ぶので、外れた試験は黙って実行されなくなる。build タグの付いたファイルも同じに見る。
func strayContractTests(file string, src any) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), file, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var stray []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		name := fn.Name.Name
		if strings.HasPrefix(name, "Test") && name != "TestMain" && !strings.HasPrefix(name, "TestContract") {
			stray = append(stray, name)
		}
	}
	return stray, nil
}

// internal/<部品>/contract*_test.go の Test 関数はすべて TestContract で始まる(internal/contract/ 自身は全件が走るので対象外)。
func TestContractTestNames(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "*", "contract*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, file := range files {
		if filepath.Base(filepath.Dir(file)) == "contract" {
			continue
		}
		checked++
		stray, err := strayContractTests(file, nil)
		if err != nil {
			t.Errorf("%s: %v", file, err)
		}
		for _, name := range stray {
			t.Errorf("%s: %s は TestContract で始まらない", file, name)
		}
	}
	if checked == 0 {
		t.Fatal("対象のファイルが 1 つも無い")
	}
}

func TestStrayContractTestsFindsOutliers(t *testing.T) {
	src := `package x
func TestContractOK(t *T) {}
func TestOther(t *T)      {}
func TestMain(m *M)       {}
func helper()             {}
func (r R) TestMethod()   {}
`
	got, err := strayContractTests("x_test.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"TestOther"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}
