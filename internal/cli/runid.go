package cli

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"time"
)

// runIDRe は run_id の形(spec 3.1.1)。--run-id の値も同じ形。
var runIDRe = regexp.MustCompile(`^r-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$`)

// newRunID は `r-<開始時刻>-<乱数>` を作る。乱数は暗号論的乱数 16 ビットの小文字の 16 進 4 文字。
// avoid に挙がった値(実行中の印・last_runs[] の run_id)と同じなら乱数を 1 回だけ作り直す(spec 3.1.1。D-0107)。
func newRunID(now time.Time, avoid func(string) bool) string {
	mk := func() string {
		var r [2]byte
		_, _ = rand.Read(r[:])
		return "r-" + now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(r[:])
	}
	id := mk()
	if avoid != nil && avoid(id) {
		id = mk()
	}
	return id
}
