package cli

// status の estimate(spec 3.1.2.7 の結果の estimate、3.1.2.8、4.1)。費用の上限は定義の値か既定、月額と上限の比較は estimate と同じ規則。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const priceUnknownDetail = `{"missing":["storage"]}`

type statusEst struct {
	result   map[string]any
	warnings []string // 今も続いている警告の原因コード(PRICE_UNKNOWN)
}

type estDef struct {
	Storage []struct {
		Endpoint string `json:"endpoint"`
	} `json:"storage"`
	ServerMonthlyYen *float64 `json:"server_monthly_yen"`
	CostCeilingYen   *float64 `json:"cost_ceiling_yen"`
}

// ceilingOf は費用の上限(定義の値。無ければ既定の 3,000。4.1)。
func ceilingOf(ctx context.Context, d *Deps) int64 {
	var def estDef
	if norm, _, err := d.State.ReadDef(ctx); err == nil {
		_ = json.Unmarshal(norm, &def)
	}
	if def.CostCeilingYen != nil {
		return int64(*def.CostCeilingYen)
	}
	return costCeilingDefaultYen
}

// statusEstimateOf: 保管先が価格表に無い・費目が無い・基本料金か最低料金のある行のときは PRICE_UNKNOWN で、月額と over_ceiling は null
// (分からないものを 0 にしない。3.1.2.8 の分岐 1)。行が引けるときの月額は、直近 30 日の backup の記録から取る入力(イテレーション 3)が
// 無いので出さない(null)。上限は常に出す。
func statusEstimateOf(ctx context.Context, d *Deps) statusEst {
	var def estDef
	if norm, _, err := d.State.ReadDef(ctx); err == nil {
		_ = json.Unmarshal(norm, &def)
	}
	uncounted := []string{"restore_dest"}
	if def.ServerMonthlyYen == nil || *def.ServerMonthlyYen <= 0 {
		uncounted = append([]string{"server"}, uncounted...)
	}
	est := statusEst{result: map[string]any{"monthly_jpy": nil, "cost_ceiling_yen": ceilingOf(ctx, d), "over_ceiling": nil, "uncounted": uncounted}}
	if d.Prices == nil || len(def.Storage) == 0 {
		return est
	}
	raw, err := d.Prices(ctx)
	var pt priceTable
	if err != nil || json.Unmarshal(raw, &pt) != nil {
		return est
	}
	row := matchStorageRow(pt, def.Storage[0].Endpoint)
	if row == nil || monthlyOf(pt, row, estInputs{Size: 1, Daily: dailyRatio, Dump: dumpRatio}, 365).total == nil { // 費目の欠けと基本料金・最低料金の行も含む
		est.warnings = append(est.warnings, string(contract.CodePriceUnknown))
	}
	return est
}
