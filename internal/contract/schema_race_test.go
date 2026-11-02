package contract

import (
	"sync"
	"testing"
)

// Validate は並行に呼べる(アダプタの実行の並行で要求・結果・フレームごとに呼ばれる)。go test -race で確かめる。
func TestValidateConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, p := range []string{"cli/envelope.json", "cli/detail.json#/$defs/INTERNAL", "adapter/notify.json#/$defs/request"} {
				if err := Validate(p, []byte(`{}`)); err == nil {
					t.Errorf("%s: 空のオブジェクトが通った", p)
				}
			}
		}()
	}
	wg.Wait()
}
