package cli

// gate の契約試験(3.1.5): 許可リストのデータ。判定(拒否のベクトルを含む)の持ち主は U1d で、internal/approval の契約試験が持つ。
// cli が判定に従って封筒と実行を決める結合の確認は U1f-2 の範囲(U1f-1 は U1d の判定を単独で満たせないため、ここには置かない)。

import (
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 許可リストは 3.1.2 の表から gate と test-tick を除いたもの。--invoked-by を受ける引数を持たない。
func TestContractCLIGateAllowlistData(t *testing.T) {
	a, err := contract.LoadGateAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, c := range a.Subcommands {
		listed[strings.Join(c.Command, " ")] = true
		for _, f := range append(append([]contract.GateFlag{}, a.CommonFlags...), c.Flags...) {
			if f.Name == "invoked-by" {
				t.Errorf("%v が --invoked-by を受ける", c.Command)
			}
		}
	}
	for _, c := range a.Excluded {
		listed[strings.Join(c.Command, " ")] = true
	}
	for _, s := range contract.SubcommandTable {
		name := strings.Join(s.Words, " ")
		want := s.Public != contract.PublicInternal && s.Public != contract.PublicTestOnly
		if listed[name] != want {
			t.Errorf("%s: 許可リストの有無 %v(公開区分 %s で %v が必要)", name, listed[name], s.Public, want)
		}
	}
}
