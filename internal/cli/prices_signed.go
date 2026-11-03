package cli

// 署名つき価格表(spec 7.2): 配布物の prices.json と prices.json.sig を /usr/libexec/sitate/prices/ から読む(bootstrap が署名を検証して置く)。
// 核は読むたびに署名(trusted comment version=<版> arch=any path=prices/prices.json を含む)を確かめ、合わないか読めないときは
// 使わず、価格表が無いのと同じに扱う(estimate は PRICE_UNKNOWN)。

import (
	"context"
	"os"
	"path/filepath"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// PricesDirDefault は価格表の置き場(spec 7.2)。
const PricesDirDefault = "/usr/libexec/sitate/prices"

// emptyPriceTable は価格表が使えないときの値(行が無い)。
const emptyPriceTable = `{"schema":"sitate.prices/v1","fx_jpy":{},"rows":[],"skipped":[]}`

type signedPrices struct {
	Dir     string
	Ver     contract.Verifier
	Version func() string // 配布物の版(動いている核の版)
}

// Load は Deps.Prices の実物。
func (p *signedPrices) Load(ctx context.Context) ([]byte, error) {
	if p.Ver == nil || p.Version == nil {
		return []byte(emptyPriceTable), nil
	}
	data, err := os.ReadFile(filepath.Join(p.Dir, "prices.json"))
	if err != nil {
		return []byte(emptyPriceTable), nil
	}
	sig, err := os.ReadFile(filepath.Join(p.Dir, "prices.json.sig"))
	if err != nil {
		return []byte(emptyPriceTable), nil
	}
	ok, err := p.Ver.Verify(ctx, data, sig, contract.TrustedComment{Version: p.Version(), Arch: BundleAnyArch, Path: "prices/prices.json"})
	if err != nil || !ok {
		return []byte(emptyPriceTable), nil
	}
	return data, nil
}
