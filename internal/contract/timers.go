package contract

import (
	"embed"
	"encoding/json"
)

// 定期実行の登録(spec 7.5 Timers、3.1.1 の規則 12)の雛形とデータ。描画と登録は U1f-1。
// 雛形は timers/ の systemd.service.tmpl・systemd.timer.tmpl・cron.tmpl(Go の text/template)、
// 表は timers/timers.json、描画の期待値は timers/golden/。

//go:embed timers
var timersFS embed.FS

const (
	// TimerSystemdTimeoutStart は systemd の TimeoutStartSec=(規則 12: 上限 + 猶予 + 記録の締め切り)。
	TimerSystemdTimeoutStart = RunMaxDuration + RunAbortGrace + RecordDeadline
	// TimerCronTerm は cron の timeout が SIGTERM を送るまで、TimerCronKillAfter はその後 SIGKILL までの猶予(-k)。
	// 合計は TimerSystemdTimeoutStart に等しい。
	TimerCronTerm      = RunMaxDuration + RunAbortGrace
	TimerCronKillAfter = RecordDeadline
	// TimerApplyMinuteOffset は apply の毎時の分を schedule の分からずらす分(spec 3.1.2 の apply の行)。
	TimerApplyMinuteOffset = 30
	// TimerInvokedBy は --invoked-by の値。
	TimerInvokedBy = "timer"

	// test-tick の引数(schema/cli/test-tick-args.json)。
	TestTickFlagDays     = "--days"
	TestTickFlagSetStart = "--set-start"
)

type TimerDefStep struct {
	Args   []string `json:"args"`
	When   string   `json:"when"`
	Retake bool     `json:"retake"`
}

type TimerDefUnit struct {
	Name         string         `json:"name"`
	Trigger      string         `json:"trigger"`
	MinuteOffset int            `json:"minute_offset"`
	Time         string         `json:"time"`
	Weekday      string         `json:"weekday"`
	Persistent   bool           `json:"persistent"`
	Steps        []TimerDefStep `json:"steps"`
}

type TimerDefTable struct {
	ServiceUser     string         `json:"service_user"`
	Binary          string         `json:"binary"`
	SystemdDir      string         `json:"systemd_dir"`
	CronFile        string         `json:"cron_file"`
	UnitPrefix      string         `json:"unit_prefix"`
	SchedulerChoice string         `json:"scheduler_choice"`
	InvokedByArgs   []string       `json:"invoked_by_args"`
	Units           []TimerDefUnit `json:"units"`
}

// LoadTimerTable は timers/timers.json を読む。
func LoadTimerTable() (TimerDefTable, error) {
	var t TimerDefTable
	b, err := timersFS.ReadFile("timers/timers.json")
	if err != nil {
		return t, err
	}
	err = json.Unmarshal(b, &t)
	return t, err
}

// TimerTemplate は雛形(名前は systemd.service.tmpl / systemd.timer.tmpl / cron.tmpl)の中身を返す。
func TimerTemplate(name string) (string, error) {
	b, err := timersFS.ReadFile("timers/" + name)
	return string(b), err
}
