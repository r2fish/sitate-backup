package main

import (
	"encoding/base64"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// resultItem は結果の errors[]・warnings[] の要素。パスの要素は path_b64 だけ(UTF-8 として正しいパスも常に path_b64。D-0119)、
// 対象全体は item_ref: null。
type resultItem struct {
	code string
	path []byte // nil のとき item_ref: null
}

func (r resultItem) MarshalJSON() ([]byte, error) {
	if r.path == nil {
		return json.Marshal(struct {
			Code    string  `json:"code"`
			ItemRef *string `json:"item_ref"`
		}{r.code, nil})
	}
	return json.Marshal(struct {
		Code    string `json:"code"`
		PathB64 string `json:"path_b64"`
	}{r.code, base64.StdEncoding.EncodeToString(r.path)})
}

// outcome は取得の集計。errors[]・warnings[] は ACQUIRE_ERRORS_MAX 件までを持ち、総件数だけ数える(メモリを件数に比例させない)。
type outcome struct {
	errs, warns   []resultItem
	nErrs, nWarns int64
	files, bytes  int64
	changed       int64
	failed        bool // 対象全体の失敗(status: failed)
	lastExit      int  // 最後の受け口の呼び出しの終了状態(end.exit_status)
}

func (o *outcome) addError(code string, path []byte) {
	o.nErrs++
	if len(o.errs) < contract.AcquireErrorsMax {
		o.errs = append(o.errs, resultItem{code, copyPath(path)})
	}
}

func (o *outcome) addWarning(code string, path []byte) {
	o.nWarns++
	if len(o.warns) < contract.AcquireErrorsMax {
		o.warns = append(o.warns, resultItem{code, copyPath(path)})
	}
}

func copyPath(p []byte) []byte {
	if p == nil {
		return nil
	}
	return append([]byte{}, p...)
}

func (o *outcome) status() string {
	switch {
	case o.failed:
		return "failed"
	case o.nErrs > 0:
		return "partial"
	}
	return "ok"
}

// resultJSON は end のフレームの直後に出す結果の 1 行。
func (o *outcome) resultJSON() []byte {
	errs, warns := o.errs, o.warns
	if errs == nil {
		errs = []resultItem{}
	}
	if warns == nil {
		warns = []resultItem{}
	}
	stats := contract.AcquireStats{
		Files: o.files, Bytes: o.bytes, Errors: o.nErrs,
		ErrorsOmitted: o.nErrs - int64(len(o.errs)), WarningsOmitted: o.nWarns - int64(len(o.warns)), ChangedDuringRead: o.changed,
	}
	b, _ := json.Marshal(struct {
		Contract string       `json:"contract"`
		Status   string       `json:"status"`
		Stats    any          `json:"stats"`
		Errors   []resultItem `json:"errors"`
		Warnings []resultItem `json:"warnings"`
	}{contract.AdapterContract["acquire"], o.status(), stats, errs, warns})
	return append(b, '\n')
}
