package prices

// 価格表の変換(spec 7.2「価格表の形」)。入力 data/prices/src/ の yaml から prices.json を作る。
// 費目(charges)は入力の文から読み取った写しを rows_*.go の表に置き、ここは入力の行の欄(事業者・出典・税・国など)と合わせて組む。

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Schema は価格表の schema 名。
const Schema = "sitate.prices/v1"

// SourceName は入力の yaml のファイル名(prices.json の source.file)。
const SourceName = "pricing-2026-11.yaml"

// 為替(spec 6.1.1。EUR は置かない)。
const fxUSDJPY = "150"

type qty struct {
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

type free struct {
	Quantity string  `json:"quantity"`
	Unit     string  `json:"unit"`
	Period   *string `json:"period"`
	Basis    string  `json:"basis"`
}

type charge struct {
	Item     string   `json:"item"`
	Ops      []string `json:"ops,omitempty"`
	Amount   string   `json:"amount"`
	Currency string   `json:"currency"`
	Unit     string   `json:"unit"`
	Per      string   `json:"per"`
	PerSrc   *string  `json:"per_src"`
	TierUpto *qty     `json:"tier_upto"`
	Free     *free    `json:"free"`
	Cond     *string  `json:"condition"`
	SrcField string   `json:"src_field"`
	SrcText  string   `json:"src_text"`
}

type minimums struct {
	StorageQuantity *qty `json:"storage_quantity"`
	StorageDays     *int `json:"storage_days"`
	ObjectSizeKB    *int `json:"object_size_kb"`
}

type srcRef struct {
	Section string `json:"section"`
	Index   int    `json:"index"`
}

type row struct {
	RowID            string            `json:"row_id"`
	Kind             string            `json:"kind"`
	Provider         string            `json:"provider"`
	Plan             string            `json:"plan"`
	Country          *string           `json:"country"`
	RegionCountry    map[string]string `json:"region_country"`
	EndpointPatterns []string          `json:"endpoint_patterns"`
	EndpointDefault  bool              `json:"endpoint_default"`
	Tax              string            `json:"tax"`
	CheckedOn        string            `json:"checked_on"`
	SourceURL        string            `json:"source_url"`
	Src              srcRef            `json:"src"`
	Minimums         minimums          `json:"minimums"`
	Charges          []charge          `json:"charges"`
}

type skipped struct {
	Src      srcRef `json:"src"`
	SrcField string `json:"src_field"`
	Reason   string `json:"reason"`
}

type table struct {
	Schema       string            `json:"schema"`
	GeneratedAt  string            `json:"generated_at"`
	Source       map[string]string `json:"source"`
	FXJPY        map[string]string `json:"fx_jpy"`
	Rows         []row             `json:"rows"`
	Combos       []any             `json:"combos"`
	RestoreDests []any             `json:"restore_dests"`
	Skipped      []skipped         `json:"skipped"`
}

// srcRow は入力の storage の 1 行(使う欄だけ)。
type srcRow struct {
	Provider         string   `yaml:"provider"`
	Product          string   `yaml:"product"`
	Tax              string   `yaml:"tax"`
	PriceDate        string   `yaml:"price_date"`
	SourceURL        string   `yaml:"source_url"`
	EndpointPatterns []string `yaml:"endpoint_patterns"`
	Country          string   `yaml:"country"`
}

type srcFile struct {
	Storage []srcRow `yaml:"storage"`
}

// rowSpec は入力の行 1 つ(または分けた 1 つ)に対する、手で写した部分。
type rowSpec struct {
	ID      string
	Src     int
	Plan    string // 空なら入力の product をそのまま。分けた行は src_text の中の名前から付ける
	Default bool
	Min     minimums
	Charges []charge
	Skipped []skipped
}

var countryRe = regexp.MustCompile(`^[A-Z]{2}$`)

// 入力の source_url の空白は %20 にする(スキーマの url は空白を許さない。spec 7.2 の (d) の例外)。

func taxOf(s string) (string, error) {
	switch {
	case strings.HasPrefix(s, "税込"):
		return "included", nil
	case strings.HasPrefix(s, "税抜"):
		return "excluded", nil
	case strings.HasPrefix(s, "該当なし"):
		return "none", nil
	}
	return "", fmt.Errorf("税の欄を読めない: %q", s)
}

func buildRow(sr srcRow, sp rowSpec) (row, error) {
	tax, err := taxOf(sr.Tax)
	if err != nil {
		return row{}, err
	}
	var country *string
	if countryRe.MatchString(sr.Country) {
		c := sr.Country
		country = &c
	}
	pats := append([]string{}, sr.EndpointPatterns...)
	plan := sp.Plan
	if plan == "" {
		plan = sr.Product
	}
	return row{
		RowID: sp.ID, Kind: "storage", Provider: sr.Provider, Plan: plan,
		Country: country, RegionCountry: map[string]string{}, EndpointPatterns: pats, EndpointDefault: sp.Default,
		Tax: tax, CheckedOn: sr.PriceDate, SourceURL: strings.ReplaceAll(sr.SourceURL, " ", "%20"), Src: srcRef{"storage", sp.Src},
		Minimums: sp.Min, Charges: sp.Charges,
	}, nil
}

// Build は入力の yaml から prices.json の中身を作る。now は generated_at。
func Build(src []byte, now time.Time) ([]byte, error) {
	var f srcFile
	if err := yaml.Unmarshal(src, &f); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(src)
	t := table{
		Schema: Schema, GeneratedAt: now.UTC().Format("2006-01-02T15:04:05Z"),
		Source: map[string]string{"file": SourceName, "sha256": hex.EncodeToString(sum[:])},
		FXJPY:  map[string]string{"USD": fxUSDJPY}, Combos: []any{}, RestoreDests: []any{}, Skipped: []skipped{},
	}
	for _, sp := range allSpecs() {
		if sp.Src >= len(f.Storage) {
			return nil, fmt.Errorf("入力の storage に %d 番目が無い", sp.Src)
		}
		r, err := buildRow(f.Storage[sp.Src], sp)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sp.ID, err)
		}
		t.Rows = append(t.Rows, r)
		t.Skipped = append(t.Skipped, sp.Skipped...)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(t); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func allSpecs() []rowSpec {
	var s []rowSpec
	s = append(s, awsSpecs()...)
	s = append(s, gcpAzureSpecs()...)
	s = append(s, otherSpecs()...)
	return s
}
