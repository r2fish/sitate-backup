//go:build mainpath

package mainpath

// 第 3 段: 試験ビルドの test-tick --days 1(作業指示書 5.18 の完了条件 6)。バックアップサーバーの容器で、実物の保管先(MinIO)とアダプタに対して
// タイマー列の 1 模擬日分を動かし、共有の値が 1 日進むことと、その間のタイマーの backup が実物で動いたことを確かめる。

import (
	"encoding/json"
	"strings"
	"time"
)

func (w *s2) tickOneDay() {
	t := w.t
	// 次の日の 00:00(Asia/Tokyo。定義の timezone の既定)に置く。共有の値は戻せないので進める向きだけ。
	loc, _ := time.LoadLocation("Asia/Tokyo")
	n := w.simNow().In(loc)
	w.setSimTime(time.Date(n.Year(), n.Month(), n.Day()+1, 0, 0, 0, 0, loc))
	before := w.simNow()
	out := w.bak.root(`runuser -u sitate -- env SITATE_SIM_TIME_FILE=/opt/sitate-test/shared/simtime SITATE_TEST_TRUST_PUBKEY_FILE=` + testTrustFn +
		` ` + sitateBin + ` test-tick --days 1`)
	t.Logf("test-tick --days 1: %.300s", out)
	after := w.simNow()
	// 共有の値は実時間でも進むので、1 日と、実行にかかった分(上限 2 分)の範囲を合格にする。
	if d := after.Sub(before); d < 24*time.Hour || d > 24*time.Hour+2*time.Minute {
		t.Errorf("共有の値が 1 日分進んでいない: %s → %s", before.Format(time.RFC3339), after.Format(time.RFC3339))
	}
	st := w.g.call("", "status").ok(t, "status(test-tick の後)")
	var r struct {
		LB struct {
			At string `json:"at"`
			OK bool   `json:"ok"`
		} `json:"last_backup"`
	}
	_ = json.Unmarshal(st.Env.Result, &r)
	at, err := time.Parse(time.RFC3339, r.LB.At)
	if err != nil || at.Before(before) || at.After(after) {
		t.Errorf("1 模擬日の間にタイマーの backup が動いていない: last_backup.at=%q(%s〜%s)", r.LB.At, before.Format(time.RFC3339), after.Format(time.RFC3339))
	}
	if !strings.Contains(string(st.Env.Result), `"last_backup"`) {
		t.Errorf("status: %s", st.Env.Result)
	}
	t.Logf("test-tick --days 1: 共有の値 %s → %s、last_backup.at=%s ok=%v", before.Format(time.RFC3339), after.Format(time.RFC3339), r.LB.At, r.LB.OK)
}
