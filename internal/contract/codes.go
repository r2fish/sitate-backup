package contract

import "strings"

import "sort"

// PublicClass は CLI サブコマンドの公開区分(spec 3.1.1)。
type PublicClass string

// 公開区分。attest は段で区分が分かれる(署名を要さない 3 段は ui-only 相当)。
const (
	PublicTool     PublicClass = "tool"
	PublicUiOnly   PublicClass = "ui-only"
	PublicTestOnly PublicClass = "test-only"
	PublicInternal PublicClass = "internal"
)

// AttestUnsignedSteps は署名を要さない attest の 3 段(道具 sitate_attest の step の値域から外す)。
var AttestUnsignedSteps = []string{"admin_access_closed", "admin_access_opened", "dest_disposed"}

func codesWhere(f func(CodeInfo) bool) []Code {
	var r []Code
	for c, i := range CodeTable {
		if f(i) {
			r = append(r, c)
		}
	}
	sort.Slice(r, func(i, j int) bool { return r[i] < r[j] })
	return r
}

// ExitClass は終了コードから error.class を返す(spec 3.1.1 の終了コードの表)。
func ExitClass(exit int) string {
	switch exit {
	case 2:
		return "input"
	case 3:
		return "precondition"
	case 4:
		return "approval"
	case 5:
		return "external"
	case 6:
		return "verification"
	case 7:
		return "conflict"
	case 9:
		return "internal"
	}
	return ""
}

// WaitingReasons は apply の waiting[].reason の値(spec 3.1.2 の apply。原因コード 5 値と applying_in_progress)。
var WaitingReasons = []string{
	"DELAY_NOT_ELAPSED", "SECRET_FD_REQUIRED", "HUMAN_SECRET_REQUIRED", "PHRASE_VERIFY_PENDING", "TEARDOWN_PENDING", "applying_in_progress",
}

// ChangeEvents は変更の記録の event の列挙(spec 4.6)。
var ChangeEvents = []string{
	"proposed", "approved", "applying", "applied", "cancelled", "expired", "failed", "committed",
}

// NotImplemented は骨格が返す共通のエラー(spec 3.1.3。終了 9、class internal、detail.feature)。
type NotImplemented struct{ Feature string }

func (e *NotImplemented) Error() string { return "NOT_IMPLEMENTED: " + e.Feature }

// ErrNotImplemented は feature の名前つきの NOT_IMPLEMENTED を返す。
// feature は NOT_IMPLEMENTED の detail.feature のスキーマ(cli/common.json の feature: 小文字で始まる)に合う形に小文字へそろえる。
func ErrNotImplemented(feature string) error {
	return &NotImplemented{Feature: strings.ToLower(feature)}
}
