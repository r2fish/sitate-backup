package main

import (
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// resultItem は結果の errors[] の要素。item_ref は `d:<target-id>:<format>`(固定値と対象 id だけ。ロール名・DB 名を使わない)か、対象全体で null。
type resultItem struct {
	code    string
	itemRef *string
}

func (r resultItem) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Code    string  `json:"code"`
		ItemRef *string `json:"item_ref"`
	}{r.code, r.itemRef})
}

// outcome は取得の集計。DB の対象の errors[] は最大でも項目数(2)以下で ACQUIRE_ERRORS_MAX を超えない。
type outcome struct {
	errs     []resultItem
	items    int   // 呼び出した項目の数
	failedN  int   // ダンプが失敗した項目の数
	okItems  int64 // 成功した項目の数
	bytes    int64 // 成功した項目の本体の合計
	failed   bool  // 対象全体の失敗(127〜254)
	lastExit int
}

func (o *outcome) status() string {
	switch {
	case o.failed || (o.items > 0 && o.failedN == o.items):
		return "failed"
	case len(o.errs) > 0:
		return "partial"
	}
	return "ok"
}

func (o *outcome) resultJSON() []byte {
	errs := o.errs
	if errs == nil {
		errs = []resultItem{}
	}
	stats := contract.AcquireStats{Files: o.okItems, Bytes: o.bytes, Errors: int64(len(errs))}
	b, _ := json.Marshal(struct {
		Contract string       `json:"contract"`
		Status   string       `json:"status"`
		Stats    any          `json:"stats"`
		Errors   []resultItem `json:"errors"`
		Warnings []resultItem `json:"warnings"`
	}{contract.AdapterContract["acquire"], o.status(), stats, errs, []resultItem{}})
	return append(b, '\n')
}
