package cli

// estimate(spec 3.1.2.8、6.1): 累積容量と月額。イテレーション 1 の範囲は「現在の構成」と --endpoint の保管先の事業者の行
// (作業指示書 5.17 の完了条件 8)。--providers の組・deltas[]・作成 / 破棄の復元先の行はイテレーション 3(A9)。
// 価格表(sitate.prices/v1)は Deps.Prices が返す。価格の値は表のものだけを使い、核は値を持たない。

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func init() { routeOwn["estimate"] = estimateEntry }

// 算定の前提の単位(spec 6.1.1。式が持つ。価格表には置かない)。
const (
	daysPerMonth     = 30.0
	extendPerMonth   = 30.0 / 7.0
	objectsPerGB     = 224.0
	sampledReads     = 3000.0
	baseRequestsMo   = 60.0
	dailyRetainDays  = 90.0
	fileCapDays      = 379.0 // 90 + 14 日と 365 + 14 日の大きい方(6.1.1 の (1))
	dumpDailyCap     = 104.0
	monthlyKeepDays  = 365.0
	dumpMonthlyDivis = 30.0
	// 引数を省いたときの目安(6.1.1 の入力: 標準例 100/1/5 と上限例 500/5/25 はどちらも対象データの 1 % と 5 %)。
	dailyRatio = 0.01
	dumpRatio  = 0.05
)

func num(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }

func toGB(q float64, unit string) float64 {
	if unit == "tb" {
		return q * 1000
	}
	return q
}

func estimateEntry(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
	if d.Prices == nil || d.State == nil {
		return notImpl("estimate")
	}
	if len(c.Args["providers"]) > 0 || len(c.Args["keep-days"]) > 0 {
		return notImpl("estimate-providers") // 組の算定と日次の保持日数の差はイテレーション 3(A9)
	}
	raw, err := d.Prices(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	var pt priceTable
	if json.Unmarshal(raw, &pt) != nil {
		return contract.Reply{}, internalErr()
	}
	if _, ok := c.Args["price-table"]; ok {
		return priceTableReply(pt)
	}
	res, warns, err := d.estimateNow(ctx, c, pt)
	if err != nil {
		return contract.Reply{}, err
	}
	b, _ := json.Marshal(res)
	return contract.Reply{Result: b, Warnings: warns}, nil
}

func argFloat(c contract.Call, name string) (float64, bool, error) {
	v := c.Args[name]
	if len(v) == 0 {
		return 0, false, nil
	}
	f, err := strconv.ParseFloat(v[0], 64)
	if err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false, argErr("--"+name, "format")
	}
	return f, true, nil
}

// estimateNow は現在の構成(--providers なし)の見積もり。
func (d *Deps) estimateNow(ctx context.Context, c contract.Call, pt priceTable) (map[string]any, []contract.Warning, error) {
	in, err := estimateInputs(c)
	if err != nil {
		return nil, nil, err
	}
	var def struct {
		Storage []struct {
			Endpoint string `json:"endpoint"`
		} `json:"storage"`
		ServerMonthlyYen *float64 `json:"server_monthly_yen"`
		CostCeilingYen   *float64 `json:"cost_ceiling_yen"`
	}
	haveDef := false
	if norm, _, e := d.State.ReadDef(ctx); e == nil && json.Unmarshal(norm, &def) == nil {
		haveDef = true
	}
	endpoint := ""
	if v := c.Args["endpoint"]; len(v) > 0 {
		endpoint = v[0] // 付けたときは定義があってもその値で引く
	} else if haveDef && len(def.Storage) > 0 {
		endpoint = def.Storage[0].Endpoint
	}
	row := matchStorageRow(pt, endpoint)
	uncounted := []string{}
	var server *float64
	if haveDef && def.ServerMonthlyYen != nil && *def.ServerMonthlyYen > 0 {
		server = def.ServerMonthlyYen
	} else {
		uncounted = append(uncounted, "server")
	}
	uncounted = append(uncounted, "restore_dest") // 復元先の月額は作成 / 破棄の組(イテレーション 4 以降)か既存ホスト(0)
	var warns []contract.Warning
	m := monthlyOf(pt, row, in, 365)
	m3 := monthlyOf(pt, row, in, 1095)
	if row == nil || m.storage == nil {
		warns = append(warns, contract.Warning{Code: contract.CodePriceUnknown, Detail: mustMarshal(map[string]any{"missing": []string{"storage"}}), Next: []contract.Next{}})
	}
	ceil, over := ceilingState(haveDef, def.CostCeilingYen, row, m.total, server)
	date := any(nil)
	if row != nil {
		date = row.CheckedOn
	}
	monthly := []any{map[string]any{"combo": "current", "y1": m.out(server), "y3": m3.out(server)}}
	basis := "args"
	res := map[string]any{
		"capacity_gb": map[string]any{"y1": capacityAt(in, 365), "y3": capacityAt(in, 1095)},
		"monthly_jpy": monthly, "deltas": []any{}, "cost_ceiling_yen": int64(ceil), "over_ceiling": over, "uncounted": uncounted,
		"excluded_suspect": 0, "price_table_date": date, "teardown_daily_jpy": 0, "basis": basis,
		"inputs": map[string]any{"size_gb": in.Size, "daily_gb": in.Daily, "dump_gb": in.Dump},
	}
	return res, warns, nil
}

// ceilingState は定義の費用の上限と、月額がそれを超えるか(定義が無い・月額が出ないときは比べず null)。
func ceilingState(haveDef bool, ceilingYen *float64, row *priceRow, total, server *float64) (float64, any) {
	if !haveDef || ceilingYen == nil {
		return 0, nil
	}
	if row == nil || total == nil {
		return *ceilingYen, nil
	}
	t := *total
	if server != nil {
		t += *server
	}
	return *ceilingYen, t > *ceilingYen
}

// estimateInputs は大きさ・日次変更・ダンプ。記録(直近 30 日の backup)から取る経路はイテレーション 3 で足し、いまは引数(basis: args)。
func estimateInputs(c contract.Call) (estInputs, error) {
	size, ok, err := argFloat(c, "size-gb")
	if err != nil {
		return estInputs{}, err
	}
	if !ok {
		return estInputs{}, argErr("--size-gb", "missing")
	}
	in := estInputs{Size: size, Daily: size * dailyRatio, Dump: size * dumpRatio}
	if v, ok, err := argFloat(c, "daily-gb"); err != nil {
		return in, err
	} else if ok {
		in.Daily = v
	}
	if v, ok, err := argFloat(c, "dump-gb"); err != nil {
		return in, err
	} else if ok {
		in.Dump = v
	}
	return in, nil
}

func hostOf(endpoint string) string {
	e := strings.TrimSpace(endpoint)
	if i := strings.Index(e, "://"); i >= 0 {
		e = e[i+3:]
	}
	if i := strings.IndexAny(e, "/?#"); i >= 0 {
		e = e[:i]
	}
	if i := strings.LastIndex(e, ":"); i >= 0 {
		e = e[:i]
	}
	return strings.ToLower(e)
}

// endpointRe は endpoint_patterns の 1 つ(<bucket>・<region> などの角括弧の語は 1 つのラベル列)を照合する式にする。
func endpointRe(p string) *regexp.Regexp {
	parts := regexp.MustCompile(`<[a-z0-9-]+>`).Split(p, -1)
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return regexp.MustCompile("^" + strings.Join(parts, `[a-z0-9-]+`) + "$")
}

// matchStorageRow は保管先の事業者の行(kind: storage)を endpoint_patterns と照合して引く。複数に当たれば endpoint_default の行、無ければ先頭。
func matchStorageRow(pt priceTable, endpoint string) *priceRow {
	host := hostOf(endpoint)
	if host == "" {
		return nil
	}
	var hit []*priceRow
	for i := range pt.Rows {
		r := &pt.Rows[i]
		if r.Kind != "storage" {
			continue
		}
		for _, p := range r.EndpointPatterns {
			if endpointRe(p).MatchString(host) {
				hit = append(hit, r)
				break
			}
		}
	}
	for _, r := range hit {
		if r.EndpointDefault {
			return r
		}
	}
	if len(hit) > 0 {
		return hit[0]
	}
	return nil
}

// priceTableReply は --price-table: 価格表の行の一覧(エージェントが server_monthly_yen を埋めるのに使う)。
func priceTableReply(pt priceTable) (contract.Reply, error) {
	rows := []map[string]any{}
	for _, r := range pt.Rows {
		var mo any
		for _, ch := range r.Charges {
			if ch.Item == "instance" && ch.Unit == "month" && ch.Currency == "JPY" {
				mo = int64(math.Round(num(ch.Amount) / math.Max(1, num(ch.Per))))
			}
		}
		pats := r.EndpointPatterns
		if pats == nil {
			pats = []string{}
		}
		rows = append(rows, map[string]any{"row_id": r.RowID, "provider": r.Provider, "plan": r.Plan, "monthly_jpy": mo, "endpoint_patterns": pats, "checked_on": r.CheckedOn})
	}
	b, _ := json.Marshal(map[string]any{"prices": rows})
	return contract.Reply{Result: b}, nil
}
