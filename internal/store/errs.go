package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/aws/smithy-go"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// newErr は原因コードの表(contract.CodeTable)から終了区分と再試行の可否を引いて型付きの誤りを作る。
func newErr(code contract.Code, detail any) *contract.Error {
	b, err := json.Marshal(detail)
	if err != nil || detail == nil {
		b = []byte(`{}`)
	}
	info := contract.CodeTable[code]
	return &contract.Error{Code: code, Class: contract.ExitClass(info.Exit), Retryable: info.Retryable, Detail: b, Next: []contract.Next{}}
}

// internalErr は結合の誤り・想定外の応答(INTERNAL。detail.component は store)。
func internalErr(reason string) *contract.Error {
	var r *string
	if reason != "" {
		r = &reason
	}
	return newErr(contract.CodeInternal, map[string]any{"component": "store", "http_status": nil, "kind": nil, "name": nil, "reason": nilReason(r)})
}

// internalStatus は保管先の想定外の 4xx(INTERNAL。detail.http_status に状態コードを残す)。
func internalStatus(status int) *contract.Error {
	return newErr(contract.CodeInternal, map[string]any{"component": "store", "http_status": status, "kind": nil, "name": nil, "reason": "contract"})
}

// statusErr は想定外の 4xx に S3 の誤りの符号を添える(契約の detail には符号の欄が無いので、包んで持つ)。
// 保持の短縮の試しが「拒否」と読む符号を限る(probe.go の triedDenied)ために使う。
type statusErr struct {
	ce     *contract.Error
	s3code string
}

func (e *statusErr) Error() string { return e.ce.Error() }
func (e *statusErr) Unwrap() error { return e.ce }

// nilReason は INTERNAL の detail.reason の列挙(exit_code / output / contract)に合わない値を contract に寄せる。
func nilReason(r *string) any {
	if r == nil {
		return nil
	}
	switch *r {
	case "exit_code", "output", "contract":
		return *r
	}
	return "contract"
}

// aborted は取り消された文脈を RUN_ABORTED にする(spec 3.1.1 の規則 11)。取り消されていなければ nil。
func aborted(ctx context.Context) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	reason := "signal"
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "deadline"
	}
	return newErr(contract.CodeRunAborted, map[string]any{"reason": reason})
}

// nilDep は依存を注入し忘れた結合の誤り(contract/harness の NilDeps が期待する INTERNAL)。
func nilDep() error { return internalErr("contract") }

// isDenied は保管先が操作を拒んだ誤り(権限またはロック)。実物は STORAGE_DENIED に写して返す。S3 の形の誤りをそのまま返す保管先の偽物も同じに扱う
// (コードを文字列の先頭で見る。AccessDenied・InvalidRequest は S3 の拒否のコード)。
func isDenied(err error) bool {
	var ce *contract.Error
	if errors.As(err, &ce) {
		return ce.Code == contract.CodeStorageDenied
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		return ae.ErrorCode() == "AccessDenied" || ae.ErrorCode() == "InvalidRequest"
	}
	return err != nil && (strings.HasPrefix(err.Error(), "AccessDenied") || strings.HasPrefix(err.Error(), "InvalidRequest"))
}

// isMissing は版が無い誤り(contract.ErrMissing、または S3 の形の NoSuchVersion・NoSuchKey)。
func isMissing(err error) bool {
	if errors.Is(err, contract.ErrMissing) {
		return true
	}
	return err != nil && (strings.HasPrefix(err.Error(), "NoSuchVersion") || strings.HasPrefix(err.Error(), "NoSuchKey"))
}

// codeOf は誤りの原因コード(contract の誤りでなければ、拒否は STORAGE_DENIED、それ以外は STORAGE_UNREACHABLE)。
func codeOf(err error) contract.Code {
	var ce *contract.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	if isDenied(err) {
		return contract.CodeStorageDenied
	}
	return contract.CodeStorageUnreachable
}
