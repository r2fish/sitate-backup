package state

import (
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

// 通知アダプタへ送る notify.test の要求は、アダプタ契約の要求のスキーマ(adapter/notify.json)に通る
// (実物のアダプタの実行器は起動の前にこれで検査し、通らなければ起動せず adapter_failed になる。主経路の第 3 段で見つかった)。
func TestTestNotifierRequestPassesAdapterSchema(t *testing.T) {
	ar := fake.NewAdapterRunner()
	ar.Script("notify", notifyOK)
	cr := &capRunner{AdapterRunner: ar}
	tn := &TestNotifier{State: &fake.StateFiles{}, Adapters: cr, Clock: &fake.Clock{T: fake.StartTime}}
	if _, err := tn.Test(ctx, []string{"a@example.invalid"}); err != nil || len(cr.reqs) != 1 {
		t.Fatalf("%v reqs=%d", err, len(cr.reqs))
	}
	if err := contract.Validate("adapter/notify.json#/$defs/request", cr.reqs[0]); err != nil {
		t.Fatalf("notify.test の要求がアダプタ契約のスキーマに通らない: %v\n%s", err, cr.reqs[0])
	}
}
