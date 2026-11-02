package cli

// estimate の算定(spec 6.1): 価格表の型、累積容量の式、費目ごとの月額(階層と無料枠)。

import (
	"math"
	"sort"
	"strings"
)

type priceCharge struct {
	Item     string   `json:"item"`
	Ops      []string `json:"ops"`
	Amount   string   `json:"amount"`
	Currency string   `json:"currency"`
	Unit     string   `json:"unit"`
	Per      string   `json:"per"`
	TierUpto *struct {
		Quantity string `json:"quantity"`
		Unit     string `json:"unit"`
	} `json:"tier_upto"`
	Free *struct {
		Quantity string `json:"quantity"`
		Unit     string `json:"unit"`
		Basis    string `json:"basis"`
	} `json:"free"`
}

type priceRow struct {
	RowID            string        `json:"row_id"`
	Kind             string        `json:"kind"`
	Provider         string        `json:"provider"`
	Plan             string        `json:"plan"`
	EndpointPatterns []string      `json:"endpoint_patterns"`
	EndpointDefault  bool          `json:"endpoint_default"`
	CheckedOn        string        `json:"checked_on"`
	Charges          []priceCharge `json:"charges"`
}

type priceTable struct {
	FX   map[string]string `json:"fx_jpy"`
	Rows []priceRow        `json:"rows"`
}

// estInputs は算定の入力(GB)。
type estInputs struct{ Size, Daily, Dump float64 }

// capacityAt は累積容量 S(t)(6.1.1 の (1)。多段保持)。
func capacityAt(in estInputs, t float64) float64 {
	g := math.Min(t, dumpDailyCap) + math.Max(0, math.Min(t, fileCapDays)-dumpDailyCap)/dumpMonthlyDivis
	return in.Size + in.Daily*math.Min(t, fileCapDays) + in.Dump*g
}

// referencedAt は参照中の容量(6.1.1 の (2))。延長の要求数の入力。
func referencedAt(in estInputs, t float64) float64 {
	g := math.Min(t, dailyRetainDays) + math.Max(0, math.Min(t, monthlyKeepDays)-dailyRetainDays)/dumpMonthlyDivis
	return in.Size + in.Daily*math.Min(t, monthlyKeepDays) + in.Dump*g
}

type monthly struct{ storage, egress, requests, total *float64 }

func (m monthly) out(server *float64) map[string]any {
	tot := m.total
	if tot != nil && server != nil {
		v := *tot + *server
		tot = &v
	}
	return map[string]any{"storage": ptr(m.storage), "egress": ptr(m.egress), "requests": ptr(m.requests), "server": ptr(server), "restore_dest": nil, "total": ptr(tot)}
}

func ptr(p *float64) any {
	if p == nil {
		return nil
	}
	return int64(math.Round(*p))
}

// monthlyOf は t 日目の月額(保管先の費目だけ)。行が無いとき・費目を円にできないときは null。
func monthlyOf(pt priceTable, row *priceRow, in estInputs, t float64) monthly {
	if row == nil {
		return monthly{}
	}
	stored := capacityAt(in, t)
	writes := (in.Daily+in.Dump)*objectsPerGB*daysPerMonth + baseRequestsMo
	refd := referencedAt(in, t)
	extend := refd*objectsPerGB*extendPerMonth + in.Size*objectsPerGB*extendPerMonth
	listing := stored * objectsPerGB / 1000 * daysPerMonth
	reads := in.Size*objectsPerGB + sampledReads // 復元テスト月 1 回
	writeClass := writes + extend + listing      // PUT・LIST の区分
	readClass := reads + writes                  // GET の区分(保持期限の読み取りを含む)
	st := itemCost(pt, row, "storage", nil, stored, stored)
	eg := itemCost(pt, row, "egress", nil, in.Size, stored) // 復元テストの egress は対象データの大きさ(月 1 回)
	rq := sumPtr(itemCost(pt, row, "request", writeOps, writeClass, stored), itemCost(pt, row, "request", readOps, readClass, stored))
	var tot *float64
	if st != nil && eg != nil && rq != nil {
		v := *st + *eg + *rq
		tot = &v
	}
	return monthly{storage: st, egress: eg, requests: rq, total: tot}
}

func sumPtr(a, b *float64) *float64 {
	if a == nil || b == nil {
		return nil
	}
	v := *a + *b
	return &v
}

var (
	// 核の操作の区分への当てはめ(spec 6.1.1 の (3))。価格表の ops[] の語: class_a は書き込み・一覧、class_b は読み出し(GCS)。
	// B2 の class_d は 0 のまま。all・R2 の無料枠・基本料金は設計の判断待ち。
	writeOps = []string{"put", "post", "copy", "list", "class_a", "write"}
	readOps  = []string{"get", "head", "select", "class_b", "read"}
)

func opsMatch(ch priceCharge, want []string) bool {
	if want == nil {
		return len(ch.Ops) == 0
	}
	for _, o := range ch.Ops {
		for _, w := range want {
			if strings.EqualFold(o, w) {
				return true
			}
		}
	}
	return false
}

// itemCost は費目 item の月額(円)。該当する費用が無ければ 0、円にできない通貨があれば null。
// 階層(tier_upto)は小さい順に使用量を割り当て、無料枠(free)は先に引く。
func itemCost(pt priceTable, row *priceRow, item string, ops []string, usage, stored float64) *float64 {
	var cs []priceCharge
	for _, ch := range row.Charges {
		if ch.Item == item && opsMatch(ch, ops) {
			cs = append(cs, ch)
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return tierBound(cs[i]) < tierBound(cs[j]) })
	total, used := 0.0, 0.0
	for _, ch := range cs {
		fx := 1.0
		switch ch.Currency {
		case "JPY":
		default:
			s, ok := pt.FX[ch.Currency]
			if !ok {
				return nil // fx_jpy に無い通貨は換算せず PRICE_UNKNOWN
			}
			fx = num(s)
		}
		amount := math.Max(0, math.Min(usage, tierBound(ch))-used)
		used += amount
		if ch.Free != nil {
			free := toGB(num(ch.Free.Quantity), ch.Free.Unit)
			if ch.Unit == "request" || ch.Free.Unit == "request" {
				free = num(ch.Free.Quantity)
			}
			if ch.Free.Basis == "stored_ratio" {
				free *= stored
			}
			amount = math.Max(0, amount-free)
		}
		per := num(ch.Per)
		if per <= 0 {
			per = 1
		}
		if ch.Unit == "tb_month" {
			amount /= 1000
		}
		total += num(ch.Amount) / per * amount * fx
	}
	return &total
}

func tierBound(ch priceCharge) float64 {
	if ch.TierUpto == nil {
		return math.MaxFloat64
	}
	q := num(ch.TierUpto.Quantity)
	if ch.Unit == "request" {
		return q
	}
	return toGB(q, ch.TierUpto.Unit)
}
