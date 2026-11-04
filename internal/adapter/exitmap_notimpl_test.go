package adapter

import (
	"errors"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 完了条件 6(5.16b): 終了 1 の NOT_IMPLEMENTED は NOT_IMPLEMENTED(detail.feature)に写すが、kind check は除く(spec 3.2.3。
// check の NOT_IMPLEMENTED は kind 固有の code として ADAPTER_ERROR に載せる)。
func TestMapAdapterErrorNotImplementedExceptKindCheck(t *testing.T) {
	ae := contract.AdapterError{Code: "NOT_IMPLEMENTED", Detail: []byte(`{"feature":"digest"}`)}
	for _, tc := range []struct {
		kind string
		want contract.Code
	}{{"notify", contract.CodeNotImplemented}, {"provision", contract.CodeNotImplemented}, {"check", contract.CodeAdapterError}} {
		_, err := mapAdapterError(tc.kind, "x", ae)
		var ce *contract.Error
		if !errors.As(err, &ce) || ce.Code != tc.want {
			t.Errorf("kind %s: %v (want %s)", tc.kind, err, tc.want)
		}
	}
}
