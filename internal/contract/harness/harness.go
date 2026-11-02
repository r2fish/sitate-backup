// Package harness は契約試験のハーネス(作業指示書 3.2)。
// 骨格は NOT_IMPLEMENTED を返し、契約試験はそれを「未実装」として失敗させる(make contract-status が集計する)。
// 7 章の機能(後のイテレーション)の契約試験は Later で skip する。
package harness

import (
	"errors"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// NotImplementedMarker は未実装の失敗を示す印(make contract-status が出力から数える)。
const NotImplementedMarker = "NOT_IMPLEMENTED:"

// LaterMarker は「後のイテレーション」の skip を示す印。
const LaterMarker = "後のイテレーション:"

// Implemented は err が NOT_IMPLEMENTED なら未実装の印つきで失敗させる。それ以外の誤りは契約どおりの失敗として通す
// (入力が零値なので、実装は ARG_INVALID など型付きの誤りを返してよい)。
func Implemented(t *testing.T, err error) {
	t.Helper()
	var ni *contract.NotImplemented
	if errors.As(err, &ni) {
		t.Fatalf("%s %s", NotImplementedMarker, ni.Feature)
	}
}

// Later は後のイテレーションの機能の契約試験を skip する(イテレーション 1 の合否に数えない)。
func Later(t *testing.T, iteration int, what string) {
	t.Helper()
	t.Skipf("%s イテレーション %d: %s", LaterMarker, iteration, what)
}
