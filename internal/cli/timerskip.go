package cli

// 導入中のタイマーの backup・restore-test の飛ばし(spec 3.1.3 の NOT_SEALED の補足、D-0028 の n2)。
// seal の前も取得・復元テストは止めない(D-0025 の 5)ので、init が登録したタイマーが前提の欠けたまま起動すると、候補の定義で走って失敗する。
// 導入中(seal の前)で前提(source enroll・restore-dest enroll の完了)が欠けている間は、タイマーから起動されたこの 2 つを本体に入らず正常に飛ばし、通知しない。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const argInvokedBy = "invoked-by"

// skipTimerSetup は c がタイマーからの起動で、seal の前に前提が欠けるなら、飛ばした結果を返す。
func (d *Deps) skipTimerSetup(ctx context.Context, c contract.Call) (contract.Reply, bool) {
	if len(c.Args[argInvokedBy]) == 0 || d.State == nil {
		return contract.Reply{}, false
	}
	var sealed bool
	if raw, err := d.State.Get(ctx, "sealed"); err == nil {
		_ = json.Unmarshal(raw, &sealed)
	}
	if sealed || d.setupComplete(ctx) {
		return contract.Reply{}, false
	}
	b, _ := json.Marshal(map[string]any{"skipped": true, "reason": "setup_incomplete"})
	return contract.Reply{Result: b}, true
}

// setupComplete は source enroll と restore-dest enroll が済んでいるか(host_keys[] の role: source と role: restore_dest、restore_dest_enrollment が null でない)。
func (d *Deps) setupComplete(ctx context.Context) bool {
	var hks []struct {
		Role string `json:"role"`
	}
	raw, err := d.State.Get(ctx, "host_keys")
	if err != nil || json.Unmarshal(raw, &hks) != nil {
		return false
	}
	var src, dst bool
	for _, h := range hks {
		src = src || h.Role == "source"
		dst = dst || h.Role == "restore_dest"
	}
	enr, err := d.State.Get(ctx, "restore_dest_enrollment")
	return src && dst && err == nil && len(enr) > 0 && string(enr) != "null"
}
