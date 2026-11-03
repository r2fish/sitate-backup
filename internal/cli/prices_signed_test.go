package cli

// 署名つき価格表の読み(spec 7.2「検証」): 読むたびに署名と trusted comment を確かめ、合わないか読めないときは使わず、
// 価格表が無いのと同じに扱う(estimate は PRICE_UNKNOWN)。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type wantVerifier struct {
	ok   bool
	got  contract.TrustedComment
	data string
}

func (v *wantVerifier) Verify(_ context.Context, target, _ []byte, want contract.TrustedComment) (bool, error) {
	v.got, v.data = want, string(target)
	return v.ok, nil
}

func writePrices(t *testing.T, sig bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prices.json"), []byte(pricesJSON("", rowWith("a", 0, "s3.example.test", cStorage, cEgress, cPut, cGet))), 0o644); err != nil {
		t.Fatal(err)
	}
	if sig {
		if err := os.WriteFile(filepath.Join(dir, "prices.json.sig"), []byte("sig"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func estimateWith(t *testing.T, p *signedPrices) []contract.Warning {
	t.Helper()
	f := estimateEnv(t)
	f.d.Prices = p.Load
	_, warns, err := estimateRun(t, f, hostArgs)
	if err != nil {
		t.Fatal(err)
	}
	return warns
}

func TestSignedPricesVerifiedTableIsUsed(t *testing.T) {
	v := &wantVerifier{ok: true}
	w := estimateWith(t, &signedPrices{Dir: writePrices(t, true), Ver: v, Version: func() string { return "9.9.9" }})
	if hasUnknown(w) {
		t.Fatalf("署名が合うのに PRICE_UNKNOWN: %+v", w)
	}
	if v.got != (contract.TrustedComment{Version: "9.9.9", Arch: "any", Path: "prices/prices.json"}) {
		t.Errorf("trusted comment の期待値: %+v", v.got)
	}
}

func TestSignedPricesBadOrMissingSignatureMeansNoTable(t *testing.T) {
	for name, p := range map[string]*signedPrices{
		"署名が合わない": {Dir: writePrices(t, true), Ver: &wantVerifier{ok: false}, Version: func() string { return "1" }},
		"署名が無い":   {Dir: writePrices(t, false), Ver: &wantVerifier{ok: true}, Version: func() string { return "1" }},
		"表が無い":    {Dir: t.TempDir(), Ver: &wantVerifier{ok: true}, Version: func() string { return "1" }},
	} {
		if w := estimateWith(t, p); !hasUnknown(w) {
			t.Errorf("%s: 表を使ってしまった(PRICE_UNKNOWN が無い): %+v", name, w)
		}
	}
}
