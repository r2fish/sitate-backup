package prices

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// 費目ごとの検査(単位・通貨・基準の取り違え、費目の欠落、minimums、endpoint_default、condition)。
// (b) は数の一致だけを定めるので、ここは入力の文の語と費目の欄(unit・currency・per・tier_upto・free・item・ops)が合うことを確かめる。

var currencyWords = map[string]string{"USD": "USD", "EUR": "EUR", "円": "JPY"}

func noComma(s string) string { return strings.ReplaceAll(s, ",", "") }

// 文に現れる通貨の語は 1 種類で、currency と一致し、amount の直後にある。
func checkCurrency(t *testing.T, id string, c charge) {
	var found []string
	for w, cur := range currencyWords {
		if strings.Contains(c.SrcText, w) {
			found = append(found, cur)
		}
	}
	if len(found) != 1 || found[0] != c.Currency {
		t.Errorf("%s: currency %q が src_text %q の通貨の語 %v と合わない", id, c.Currency, c.SrcText, found)
	}
	re := regexp.MustCompile(`(^|[^0-9.,])` + regexp.QuoteMeta(c.Amount) + `\s*(USD|EUR|円)`)
	m := re.FindStringSubmatch(noComma(c.SrcText))
	if m == nil || currencyWords[m[2]] != c.Currency {
		t.Errorf("%s: amount %q の直後に通貨の語 %q が無い: %q", id, c.Amount, c.Currency, c.SrcText)
	}
}

// 欄の名前と item、文の「/TB」から unit が決まる。per は最初の「/<数>」、無ければ 1。
func checkUnitPer(t *testing.T, id string, c charge) {
	want := map[string]string{"storage": "gb_month", "egress": "gb", "retrieval": "gb", "request": "request"}[c.Item]
	if strings.Contains(c.SrcText, "/TB") && c.Item != "request" {
		want = "tb_month"
	}
	if c.Unit != want {
		t.Errorf("%s: unit %q != %q (item %s, src_text %q)", id, c.Unit, want, c.Item, c.SrcText)
	}
	wantField := map[string]string{"storage": fStore, "egress": fEgress, "retrieval": fEgress, "request": fReq}[c.Item]
	if c.SrcField != wantField {
		t.Errorf("%s: item %s の src_field %q != %q", id, c.Item, c.SrcField, wantField)
	}
	wantPer := "1"
	if m := regexp.MustCompile(`/\s*([0-9][0-9,]*)`).FindStringSubmatch(c.SrcText); m != nil {
		wantPer = noComma(m[1])
	}
	if c.Per != wantPer {
		t.Errorf("%s: per %q != %q (src_text %q)", id, c.Per, wantPer, c.SrcText)
	}
}

// retrieval は、入力の欄で src_text を含む文(「。」まで)に「取り出し料」があるときだけ。
func checkRetrieval(t *testing.T, id, field string, c charge) {
	if c.Item == "request" || c.Item == "storage" {
		return
	}
	i := strings.Index(field, c.SrcText)
	seg := field[strings.LastIndex(field[:i], "。")+1 : i+len(c.SrcText)]
	if has := strings.Contains(seg, "取り出し料"); has != (c.Item == "retrieval") {
		t.Errorf("%s: item %q だが文の「取り出し料」は %v: %q", id, c.Item, has, seg)
	}
}

var opWords = map[string][]string{
	"class_a": {"Class A"}, "class_b": {"Class B"}, "class_d": {"Class D"},
	"PUT": {"PUT"}, "COPY": {"COPY"}, "POST": {"POST"}, "LIST": {"LIST"}, "GET": {"GET"},
	"write": {"書き込み"}, "read": {"読み取り"}, "list": {"一覧"}, "other": {"その他"},
}

// ops の各値は文に語がある。all は区分の語が文に 1 つも無いときだけ。
func checkOps(t *testing.T, id string, c charge) {
	if c.Item != "request" {
		return
	}
	if len(c.Ops) == 1 && c.Ops[0] == "all" {
		for op, ws := range opWords {
			if strings.Contains(c.SrcText, ws[0]) {
				t.Errorf("%s: ops all だが src_text に区分の語 %s がある: %q", id, op, c.SrcText)
			}
		}
		return
	}
	for _, op := range c.Ops {
		ws, ok := opWords[op]
		if !ok || !strings.Contains(c.SrcText, ws[0]) {
			t.Errorf("%s: ops %q の語が src_text に無い: %q", id, op, c.SrcText)
		}
	}
	for op, ws := range opWords {
		in := false
		for _, o := range c.Ops {
			in = in || o == op
		}
		if !in && strings.Contains(c.SrcText, ws[0]) {
			t.Errorf("%s: src_text の区分の語 %s が ops %v に無い: %q", id, op, c.Ops, c.SrcText)
		}
	}
}

// 無料の費目(規則 6)の ops: 「Class A・B・C」は 3 区分、区分の語が無ければ all。
func checkFreeOps(t *testing.T, id string, c charge) {
	want := []string{"all"}
	if strings.Contains(c.SrcText, "Class A・B・C") {
		want = []string{"class_a", "class_b", "class_c"}
	}
	for _, o := range c.Ops {
		ok := false
		for _, w := range want {
			ok = ok || o == w
		}
		if !ok || len(c.Ops) != 1 {
			t.Errorf("%s: 無料の費目の ops %v が %v の 1 つでない: %q", id, c.Ops, want, c.SrcText)
		}
	}
}

var unitWords = map[string]string{"gb": `(GB|GiB)`, "tb": `(TB|TiB)`, "request": `件`}

// tier_upto の直後は TB / TiB。free の直後の語が unit と合い、「倍」があるときだけ stored_ratio。
func checkTierFree(t *testing.T, id string, c charge) {
	s := noComma(c.SrcText)
	if c.TierUpto != nil && !nextTierRe.MatchString(s) { // 幅の段は TestPricesCharges が累積を確かめる
		re := regexp.MustCompile(`(^|[^0-9.])` + c.TierUpto.Quantity + `\s*(TB|TiB)`)
		if c.TierUpto.Unit != "tb" || !re.MatchString(s) {
			t.Errorf("%s: tier_upto %v の直後が TB / TiB でない: %q", id, *c.TierUpto, c.SrcText)
		}
	}
	if c.Free == nil {
		return
	}
	ratio := strings.Contains(s, "倍")
	if ratio != (c.Free.Basis == "stored_ratio") {
		t.Errorf("%s: free.basis %q と「倍」(%v) が合わない: %q", id, c.Free.Basis, ratio, c.SrcText)
	}
	suffix := unitWords[c.Free.Unit]
	if ratio {
		suffix = "倍"
	}
	re := regexp.MustCompile(`(^|[^0-9.])` + c.Free.Quantity + `\s*` + suffix)
	if !re.MatchString(s) {
		t.Errorf("%s: free %v の直後の語が unit と合わない: %q", id, *c.Free, c.SrcText)
	}
}

func TestPricesChargeUnitsAndCurrency(t *testing.T) {
	src := loadSrc(t)
	for _, r := range loadTable(t).Rows {
		for _, c := range r.Charges {
			if isFreeCharge(c) {
				checkFreeOps(t, r.RowID, c)
				continue
			}
			checkCurrency(t, r.RowID, c)
			checkUnitPer(t, r.RowID, c)
			checkRetrieval(t, r.RowID, inputText(t, src, r, c.SrcField), c)
			checkOps(t, r.RowID, c)
			checkTierFree(t, r.RowID, c)
		}
	}
}

// 入力の価格の欄の数は、その数を含む src_text の費目が amount・per・tier_upto・free のどれかで使うか、理由つきの許可リストにある。
// src_text に含まれない部分(段ごと消した費目)の数も同じ扱い。
type exempt struct{ snippet, reason string }

var unusedNumbers = map[[2]any][]exempt{
	{0, fStore}:   {{"500 TB 超", "超過の境界(最後の段は tier_upto を持たない)"}},
	{0, fEgress}:  {{"150 TB 超", "超過の境界"}},
	{1, fEgress}:  {{"150 TB 超", "超過の境界"}},
	{2, fEgress}:  {{"150 TB 超", "超過の境界"}},
	{3, fEgress}:  {{"0〜10 TiB", "範囲の下端"}, {"10〜150 TiB", "範囲の下端(段の上端 150 が tier_upto)"}, {"150 TiB 超", "超過の境界"}},
	{4, fEgress}:  {{"0〜10 TiB", "範囲の下端"}, {"10〜150 TiB", "範囲の下端(段の上端 150 が tier_upto)"}, {"150 TiB 超", "超過の境界"}},
	{5, fStore}:   {{"500 TB 超", "超過の境界"}},
	{5, fEgress}:  {{"インターネット経路 0.11 USD(〜約 10 TB)、0.075 USD(〜約 50 TB)、0.07 USD(〜約 150 TB)、0.06 USD(以降)", "写していない課金(インターネット経路。額は見積もりに入らない)"}},
	{6, fEgress}:  {{"インターネット経路 0.11 USD(〜約 10 TB)、0.075 USD(〜約 50 TB)、0.07 USD(〜約 150 TB)、0.06 USD(以降)", "写していない課金(インターネット経路)"}},
	{7, fReq}:     {{"1 日", "無料枠の期間(free.period が day)"}},
	{9, fReq}:     {{"Class A 100 万件、Class B 1,000 万件は無料", "R2 の無料枠は「万件」の表記で condition に置く(判断待ち)"}},
	{10, fStore}:  {{"公式カタログ API では 0.000022 EUR/GB", "別の出所の参考値"}},
	{11, fStore}:  {{"基本料金に 1 TB を含む。超過 0.0087 EUR/TB 時", "時間単位の課金は写さない(unmapped)"}},
	{11, fReq}:    {{"S3 API", "名前の中の数"}},
	{14, fReq}:    {{"別にオブジェクト数は基本料金に 100,000 個を含み、超過 11 円/10,000 個", "オブジェクト数の課金は写さない(skipped の unmapped)"}},
	{15, fStore}:  {{"は 0 USD", "無料枠の単価 0(free で表す)"}},
	{15, fEgress}: {{"は 0 USD", "無料枠の単価 0(free で表す)"}},
}

func exempted(text string, k [2]any, pos int) bool {
	for _, e := range unusedNumbers[k] {
		if i := strings.Index(text, e.snippet); i >= 0 && pos >= i && pos < i+len(e.snippet) {
			return true
		}
	}
	return false
}

func TestPricesEveryNumberUsed(t *testing.T) {
	src := loadSrc(t)
	tb := loadTable(t)
	type cover struct {
		lo, hi int
		nums   map[string]bool
	}
	covered := map[[2]any][]cover{}
	for _, r := range tb.Rows {
		for _, c := range r.Charges {
			k := [2]any{r.Src.Index, c.SrcField}
			i := strings.Index(str(src[r.Src.Index], c.SrcField), c.SrcText)
			own := map[string]bool{c.Amount: true, c.Per: true}
			if c.TierUpto != nil {
				own[c.TierUpto.Quantity] = true
				if m := nextTierRe.FindStringSubmatch(c.SrcText); m != nil {
					own[noComma(m[1])] = true // 幅で書く段は累積にした値と幅の両方が使われた数
				}
			}
			if c.Free != nil {
				own[c.Free.Quantity] = true
			}
			covered[k] = append(covered[k], cover{i, i + len(c.SrcText), own})
		}
	}
	for i, in := range src {
		for _, f := range []string{fStore, fEgress, fReq} {
			text := str(in, f)
			k := [2]any{i, f}
			if strings.HasPrefix(text, "未確認") {
				continue // skipped[] の unconfirmed(TestPricesSkippedReasons が確かめる)
			}
			for _, loc := range numRe.FindAllStringIndex(text, -1) {
				n := noComma(text[loc[0]:loc[1]])
				if inComputedSentence(text, loc[0]) {
					continue // 「(算定)」の値と前提は写さず skipped[] の computed に置く(spec 7.2)
				}
				consumed := false
				for _, cv := range covered[k] { // 数を含む src_text の費目自身が使っていること
					consumed = consumed || (loc[0] >= cv.lo && loc[1] <= cv.hi && cv.nums[n])
				}
				if consumed {
					continue
				}
				if !exempted(text, k, loc[0]) {
					t.Errorf("storage[%d] の %s の数 %s が費目の数として使われていない(許可リストにも無い): %q", i, f, n, text)
				}
			}
		}
	}
}

// (c) 入力の minimums が「該当なし」で始まらない行は、行の minimums に値があるか skipped[] に現れる。
func TestPricesMinimumsAppear(t *testing.T) {
	src := loadSrc(t)
	tb := loadTable(t)
	has := map[int]bool{}
	for _, r := range tb.Rows {
		m := r.Minimums
		if m.StorageQuantity != nil || m.StorageDays != nil || m.ObjectSizeKB != nil {
			has[r.Src.Index] = true
		}
	}
	for _, s := range tb.Skipped {
		if s.SrcField == "minimums" {
			has[s.Src.Index] = true
		}
	}
	for i, in := range src {
		if i > 15 || i >= len(src) {
			break
		}
		if m := str(in, "minimums"); m != "" && !strings.HasPrefix(m, "該当なし") && !has[i] {
			t.Errorf("(c) storage[%d] (%s) の minimums %q が行にも skipped にも無い", i, str(in, "product"), m)
		}
	}
}

// endpoint_default の期待値: 見積もりが同じ endpoint で最初に引く行(標準の区分)だけ true。
func TestPricesEndpointDefault(t *testing.T) {
	want := map[string]bool{
		"aws-s3-standard-apne1": true, "gcp-gcs-standard-asne1": true, "azure-blob-hot-japaneast": true,
		"r2-standard": true, "scaleway-os-standard-par": true, "storj-standard": true, "oci-os-standard": true,
	}
	byPat := map[string]int{}
	for _, r := range loadTable(t).Rows {
		if r.EndpointDefault != want[r.RowID] {
			t.Errorf("%s: endpoint_default %v != %v", r.RowID, r.EndpointDefault, want[r.RowID])
		}
		if r.EndpointDefault {
			byPat[strings.Join(r.EndpointPatterns, "|")]++
		}
	}
	for p, n := range byPat {
		if n != 1 {
			t.Errorf("同じ endpoint_patterns %q に default が %d 行", p, n)
		}
	}
}

// condition は入力の文のまま(src_field の欄の部分文字列)。
func TestPricesConditionFromInput(t *testing.T) {
	src := loadSrc(t)
	n := 0
	for _, r := range loadTable(t).Rows {
		for _, c := range r.Charges {
			if c.Cond == nil {
				continue
			}
			n++
			if !strings.Contains(inputText(t, src, r, c.SrcField), *c.Cond) {
				t.Errorf("%s: condition %q が %s の部分文字列でない", r.RowID, *c.Cond, c.SrcField)
			}
		}
	}
	if n == 0 {
		t.Error("condition を持つ費目が 1 つも無い")
	}
}

// inComputedSentence は pos を含む「。」区切りの文に「算定」があるか。
func inComputedSentence(text string, pos int) bool {
	start := strings.LastIndex(text[:pos], "。") + 1
	end := len(text)
	if e := strings.Index(text[pos:], "。"); e >= 0 {
		end = pos + e
	}
	return strings.Contains(text[start:end], "算定")
}

// 規則 6(D-0159): 入力が無料と明記する区分は amount 0 の費目。B2 は class_a・class_b・class_c が 0 で class_d と無料枠が残り、
// Wasabi(区分なし)・Hetzner・IDrive・Storj は ops all が 0。
func TestPricesFreeRequestCharges(t *testing.T) {
	byID := map[string]row{}
	for _, r := range loadTable(t).Rows {
		byID[r.RowID] = r
	}
	zeroOps := func(id string) map[string]bool {
		m := map[string]bool{}
		for _, c := range byID[id].Charges {
			if c.Item == "request" && c.Amount == "0" {
				for _, o := range c.Ops {
					m[o] = true
				}
			}
		}
		return m
	}
	b2 := zeroOps("b2-payg")
	for _, o := range []string{"class_a", "class_b", "class_c"} {
		if !b2[o] {
			t.Errorf("b2-payg に %s の amount 0 の費目が無い", o)
		}
	}
	d := 0
	for _, c := range byID["b2-payg"].Charges {
		if c.Item == "request" && len(c.Ops) == 1 && c.Ops[0] == "class_d" && c.Amount == "0.004" && c.Free != nil && c.Free.Quantity == "2500" {
			d++
		}
	}
	if d != 1 {
		t.Errorf("b2-payg の class_d の費目と無料枠が残っていない(%d)", d)
	}
	for _, id := range []string{"wasabi-hot-payg", "hetzner-os", "idrive-e2-payg", "storj-standard"} {
		if !zeroOps(id)["all"] && id != "wasabi-hot-payg" {
			t.Errorf("%s に ops all の amount 0 の費目が無い", id)
		}
	}
	if !zeroOps("wasabi-hot-payg")["all"] {
		t.Error("wasabi-hot-payg に ops all の amount 0 の費目が無い")
	}
}

// 規則 7(D-0159): 月額の基本料金か最低料金がある 5 行(Wasabi・Hetzner・IDrive・Storj・さくら)だけが minimums を skipped[](unmapped)に置く。
func TestPricesMinimumFeeRowsSkipped(t *testing.T) {
	tb := loadTable(t)
	got := map[int]bool{}
	for _, s := range tb.Skipped {
		if s.SrcField == "minimums" && s.Reason == "unmapped" {
			got[s.Src.Index] = true
		}
	}
	want := map[int]bool{8: true, 11: true, 12: true, 13: true, 14: true}
	for i := range want {
		if !got[i] {
			t.Errorf("storage[%d] の minimums が skipped[](unmapped)に無い", i)
		}
	}
	for i := range got {
		if !want[i] {
			t.Errorf("storage[%d] は最低料金の行でないのに minimums が unmapped", i)
		}
	}
}

// skipped[] の全件(入力の欄ごとの写していない理由)。増減は設計の確認が要る。
func TestPricesSkippedExact(t *testing.T) {
	want := map[string]bool{
		"10 request_price unconfirmed": true, "10 minimums unconfirmed": true,
		"11 storage_price_per_gb_month unmapped": true, "11 storage_price_per_gb_month computed": true,
		"8 minimums unmapped": true, "11 minimums unmapped": true, "12 minimums unmapped": true,
		"13 minimums unmapped": true, "14 request_price unmapped": true, "14 minimums unmapped": true,
	}
	got := map[string]bool{}
	for _, s := range loadTable(t).Skipped {
		got[fmt.Sprintf("%d %s %s", s.Src.Index, s.SrcField, s.Reason)] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("skipped に %q が無い", k)
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("想定外の skipped: %q", k)
		}
	}
}
