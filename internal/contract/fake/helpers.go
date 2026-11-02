package fake

import (
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// ManualClock は偽の保管先の時計(TimeSource)。契約試験が Set・Advance で進める。
type ManualClock struct{ T time.Time }

// Now は TimeSource を満たす。
func (c *ManualClock) Now() (time.Time, error) { return c.T, nil }

// Advance は時計を d 進める。
func (c *ManualClock) Advance(d time.Duration) { c.T = c.T.Add(d) }

// StartTime は契約試験の既定の開始時刻。
var StartTime = time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)

// NewTestStore はバケット "b"(Object Lock 有効)をもつ偽の保管先を作り、role の鍵の ObjectStore と時計を返す。
func NewTestStore(role Role) (*ObjectStore, *ManualClock, *Core) {
	clk := &ManualClock{T: StartTime}
	core := NewCore(clk)
	if err := core.CreateBucket("b", true); err != nil {
		panic(err)
	}
	return &ObjectStore{Core: core, Bucket: "b", Role: role, Params: contract.FixedRetention{}}, clk, core
}
