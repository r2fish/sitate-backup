package acquire

// failed の対象の next[](spec 3.1.2 の targets[]、3.1.3 の原因コードの表の next の列。D-0172・D-0179)。
// 核は表の next の列を型として持たないため、対象の失敗になり得る原因コードの分をここに置く。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// withNext は failed の対象に、その原因コードの next[] を入れる。
// 表にある原因コード(STORAGE_UNREACHABLE・RECEIVER_TARGETS_MISMATCH・INTERNAL)だけが値を持ち、
// errors[].code 由来の値(NOT_FOUND・PERMISSION・EXIT_STATUS)、ADAPTER_ERROR、ok・partial では空の配列にする。
func (a *Acquire) withNext(ctx context.Context, out TargetOutcome) TargetOutcome {
	out.Next = []contract.Next{}
	if out.Status != "failed" {
		return out
	}
	code := contract.Code(out.ErrorCode)
	switch code {
	case contract.CodeStorageUnreachable:
		out.Next = []contract.Next{nextOf(code, false, "sitate", "doctor")}
	case contract.CodeReceiverTargetsMismatch: // seal の前: source enroll、seal の後: propose source-change
		if sealed, _ := getBool(ctx, a.State, "sealed"); sealed {
			out.Next = []contract.Next{nextOf(code, false, "sitate", "propose", "source-change")}
		} else {
			out.Next = []contract.Next{nextOf(code, false, "sitate", "source", "enroll")}
		}
	case contract.CodeInternal: // component が adapter のときは [](アダプタの提供元の案内を確かめる。アプリのカタログの文)
		var d struct {
			Component string `json:"component"`
		}
		_ = json.Unmarshal(out.Detail, &d)
		if d.Component != "adapter" {
			out.Next = []contract.Next{nextOf(code, false, "sitate", "status")}
		}
	}
	return out
}
