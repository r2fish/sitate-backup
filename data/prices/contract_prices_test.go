package prices

import (
	"os"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// 価格表の契約試験(作業指示書 3.2)。骨格の未実装の印(data/prices/UNIMPLEMENTED)がある間は「未実装」で失敗する。
// U5 は印のファイルを消して価格表のスキーマと署名の検証の試験を足す。この試験は変えない。
func TestContractPricesImplemented(t *testing.T) {
	if _, err := os.Stat("UNIMPLEMENTED"); err == nil {
		t.Fatalf("%s data/prices は骨格のまま", harness.NotImplementedMarker)
	}
}
