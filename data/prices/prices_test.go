package prices

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"gopkg.in/yaml.v3"
)

// 価格表の試験(作業指示書 5.15)。入力 src/ の各行と変換後の値の一致(spec 7.2 の「一致」の定義 (a)〜(d))と、スキーマ。

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 入力の storage の行を欄の名前から文字列で引く形で読む。
func loadSrc(t *testing.T) []map[string]any {
	t.Helper()
	var f struct {
		Storage []map[string]any `yaml:"storage"`
	}
	if err := yaml.Unmarshal(readFile(t, "src/"+SourceName), &f); err != nil {
		t.Fatal(err)
	}
	return f.Storage
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func loadTable(t *testing.T) table {
	t.Helper()
	var tb table
	if err := json.Unmarshal(readFile(t, "prices.json"), &tb); err != nil {
		t.Fatal(err)
	}
	return tb
}

func TestPricesSchema(t *testing.T) {
	if err := contract.Validate("data/prices.json", readFile(t, "prices.json")); err != nil {
		t.Fatalf("prices.json がスキーマに合わない: %v", err)
	}
}

// 入力のバイト列の SHA-256 と、変換し直した結果(generated_at を除く)が、置いてある prices.json と同じ。
func TestPricesUpToDate(t *testing.T) {
	src := readFile(t, "src/"+SourceName)
	b, err := Build(src, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	norm := func(x []byte) string {
		return regexp.MustCompile(`"generated_at": "[^"]*"`).ReplaceAllString(string(x), `"generated_at": ""`)
	}
	if norm(b) != norm(readFile(t, "prices.json")) {
		t.Fatal("prices.json が入力と食い違う(go run ./data/prices/gen で作り直す)")
	}
	if tb := loadTable(t); len(tb.Source["sha256"]) != 64 || tb.FXJPY["USD"] != "150" || tb.FXJPY["EUR"] != "" {
		t.Fatalf("source / fx_jpy が不正: %v %v", tb.Source, tb.FXJPY)
	}
}

var numRe = regexp.MustCompile(`[0-9][0-9,]*(\.[0-9]+)?`)

// nums は文字列に現れる数(桁区切りのカンマを除いた 10 進の表記)の集合。
func nums(s string) map[string]bool {
	m := map[string]bool{}
	for _, n := range numRe.FindAllString(s, -1) {
		m[strings.ReplaceAll(n, ",", "")] = true
	}
	return m
}

var slashDigit = regexp.MustCompile(`/\s*[0-9]`)

func inputText(t *testing.T, src []map[string]any, r row, field string) string {
	t.Helper()
	if r.Src.Index >= len(src) {
		t.Fatalf("%s: src.index %d が入力に無い", r.RowID, r.Src.Index)
	}
	return str(src[r.Src.Index], field)
}

// (a) src_text と per_src は、src が指す入力の行の src_field の欄の部分文字列。
// (b) 数の欄は src_text に現れる数と文字列として等しい。文に数の無い量(per の 1)は 1 とし、文に「/<数>」が無いこと。
func TestPricesCharges(t *testing.T) {
	src := loadSrc(t)
	for _, r := range loadTable(t).Rows {
		for i, c := range r.Charges {
			field := inputText(t, src, r, c.SrcField)
			if !strings.Contains(field, c.SrcText) {
				t.Errorf("(a) %s: src_text %q が %s の部分文字列でない", r.RowID, c.SrcText, c.SrcField)
			}
			if c.PerSrc != nil && !strings.Contains(field, *c.PerSrc) {
				t.Errorf("(a) %s: per_src %q が部分文字列でない", r.RowID, *c.PerSrc)
			}
			if c.Item == "request" && len(c.Ops) == 0 {
				t.Errorf("%s: request の ops が空", r.RowID)
			}
			if isFreeCharge(c) { // 規則 6: amount 0 は無料を言う語が src_text にあること。per は 1
				if c.Per != "1" {
					t.Errorf("(b) %s: 無料の費目の per %q が 1 でない", r.RowID, c.Per)
				}
				continue
			}
			n := nums(c.SrcText)
			if !n[c.Amount] {
				t.Errorf("(b) %s: amount %q が src_text %q に無い", r.RowID, c.Amount, c.SrcText)
			}
			if c.PerSrc == nil && !n[c.Per] && !(c.Per == "1" && !slashDigit.MatchString(c.SrcText)) {
				t.Errorf("(b) %s: per %q が src_text %q に無い", r.RowID, c.Per, c.SrcText)
			}
			if c.TierUpto != nil {
				checkTierQty(t, r, i, n)
			}
			if c.Free != nil && !n[c.Free.Quantity] {
				t.Errorf("(b) %s: free.quantity %q が src_text %q に無い", r.RowID, c.Free.Quantity, c.SrcText)
			}
		}
	}
}

var freeWords = []string{"無料", "No Charges", "料金なし", "No API fees"}

// isFreeCharge は規則 6 の費目(入力が無料と明記する区分の amount 0)。
func isFreeCharge(c charge) bool {
	if c.Item != "request" || c.Amount != "0" {
		return false
	}
	for _, w := range freeWords {
		if strings.Contains(c.SrcText, w) {
			return true
		}
	}
	return false
}

var nextTierRe = regexp.MustCompile(`次の\s*([0-9,]+)\s*(TB|TiB)`)

// checkTierQty は tier_upto.quantity。入力が幅で書く段(「次の N」)は累積(前の段の上限 + N)、それ以外は src_text に現れる数と等しい(spec 7.2 の (b) の例外)。
func checkTierQty(t *testing.T, r row, i int, n map[string]bool) {
	c := r.Charges[i]
	m := nextTierRe.FindStringSubmatch(c.SrcText)
	if m == nil {
		if !n[c.TierUpto.Quantity] {
			t.Errorf("(b) %s: tier_upto %q が src_text %q に無い", r.RowID, c.TierUpto.Quantity, c.SrcText)
		}
		return
	}
	prev := 0.0
	for j := i - 1; j >= 0; j-- {
		if p := r.Charges[j]; p.Item == c.Item && p.TierUpto != nil {
			prev, _ = strconv.ParseFloat(p.TierUpto.Quantity, 64)
			break
		}
	}
	w, _ := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if got, _ := strconv.ParseFloat(c.TierUpto.Quantity, 64); got != prev+w {
		t.Errorf("(b) %s: 幅の段 %q の tier_upto %q が累積 %v でない", r.RowID, c.SrcText, c.TierUpto.Quantity, prev+w)
	}
}

// (b) minimums の数は入力の minimums に現れる数と等しい。
func TestPricesMinimums(t *testing.T) {
	src := loadSrc(t)
	for _, r := range loadTable(t).Rows {
		n := nums(inputText(t, src, r, "minimums"))
		m := r.Minimums
		if m.StorageQuantity != nil && !n[m.StorageQuantity.Quantity] {
			t.Errorf("(b) %s: storage_quantity %q が minimums に無い", r.RowID, m.StorageQuantity.Quantity)
		}
		for name, v := range map[string]*int{"storage_days": m.StorageDays, "object_size_kb": m.ObjectSizeKB} {
			if v != nil && !n[itoa(*v)] {
				t.Errorf("(b) %s: %s %d が minimums に無い", r.RowID, name, *v)
			}
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// (c) 入力の各行の価格の欄は、どれかの費目の src_field か skipped[] に現れる(minimums は行の minimums)。
// イテレーション 1 は storage の全行を写す(compute の行はイテレーション 3)。
func TestPricesEveryInputFieldAppears(t *testing.T) {
	src := loadSrc(t)
	tb := loadTable(t)
	rowOf := map[int]bool{}
	seen := map[[2]any]bool{}
	for _, r := range tb.Rows {
		rowOf[r.Src.Index] = true
		for _, c := range r.Charges {
			seen[[2]any{r.Src.Index, c.SrcField}] = true
		}
	}
	for _, s := range tb.Skipped {
		seen[[2]any{s.Src.Index, s.SrcField}] = true
	}
	for i, in := range src {
		if !rowOf[i] {
			t.Errorf("(c) 入力の storage[%d] (%s) の行が無い", i, str(in, "product"))
			continue
		}
		for _, f := range []string{fStore, fEgress, fReq} {
			if str(in, f) != "" && !seen[[2]any{i, f}] {
				t.Errorf("(c) storage[%d] の %s が費目にも skipped にも無い", i, f)
			}
		}
	}
	for _, s := range tb.Skipped {
		if !map[string]bool{"unconfirmed": true, "computed": true, "not_applicable": true, "unmapped": true}[s.Reason] {
			t.Errorf("skipped の reason が不正: %v", s)
		}
	}
}

// skipped の理由が入力と合う: unconfirmed は欄の文が「未確認」で始まる。
func TestPricesSkippedReasons(t *testing.T) {
	src := loadSrc(t)
	for _, s := range loadTable(t).Skipped {
		text := str(src[s.Src.Index], s.SrcField)
		switch s.Reason {
		case "unconfirmed":
			if !strings.HasPrefix(text, "未確認") {
				t.Errorf("unconfirmed なのに「未確認」で始まらない: %v %q", s, text)
			}
		case "computed":
			if !strings.Contains(text, "算定") {
				t.Errorf("computed なのに「算定」が無い: %v %q", s, text)
			}
		case "not_applicable":
			if !strings.Contains(text, "無料") && !strings.Contains(text, "なし") {
				t.Errorf("not_applicable なのに「無料」「なし」が無い: %v %q", s, text)
			}
		}
	}
}

// (d) provider・endpoint_patterns・checked_on・source_url と分けていない行の plan は入力と等しい。
// 例外は source_url の空白の %20 だけ(spec 7.2。D-0159)。provider と endpoint_patterns は文字列として等しい。
func TestPricesRowFieldsMatchInput(t *testing.T) {
	src := loadSrc(t)
	split := map[int]int{}
	tb := loadTable(t)
	for _, r := range tb.Rows {
		split[r.Src.Index]++
	}
	ids := map[string]bool{}
	for _, r := range tb.Rows {
		in := src[r.Src.Index]
		if ids[r.RowID] {
			t.Errorf("row_id が重複: %s", r.RowID)
		}
		ids[r.RowID] = true
		if r.Provider != str(in, "provider") {
			t.Errorf("(d) %s: provider %q != %q", r.RowID, r.Provider, str(in, "provider"))
		}
		if r.CheckedOn != str(in, "price_date") || r.SourceURL != strings.ReplaceAll(str(in, "source_url"), " ", "%20") {
			t.Errorf("(d) %s: checked_on / source_url が入力と違う", r.RowID)
		}
		if split[r.Src.Index] == 1 && r.Plan != str(in, "product") {
			t.Errorf("(d) %s: plan %q != product", r.RowID, r.Plan)
		}
		var pats []string
		for _, p := range in["endpoint_patterns"].([]any) {
			pats = append(pats, p.(string))
		}
		if strings.Join(pats, "|") != strings.Join(r.EndpointPatterns, "|") {
			t.Errorf("(d) %s: endpoint_patterns が入力と違う: %v", r.RowID, r.EndpointPatterns)
		}
		country := str(in, "country")
		if (r.Country != nil) != regexp.MustCompile(`^[A-Z]{2}$`).MatchString(country) || (r.Country != nil && *r.Country != country) {
			t.Errorf("(d) %s: country %v が入力 %q と合わない", r.RowID, r.Country, country)
		}
		wantTax := map[string]string{"税込": "included", "税抜": "excluded", "該当なし": "none"}
		for k, v := range wantTax {
			if strings.HasPrefix(str(in, "tax"), k) && r.Tax != v {
				t.Errorf("%s: tax %q != %q", r.RowID, r.Tax, v)
			}
		}
	}
}
