package state

// タイマーの登録(spec 7.5 Timers、3.1.1 の規則 12): 表(contract の timers/timers.json)と雛形から systemd の単位か cron の記述を描画して Dir へ書く。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"context"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Timers は contract.Timers の実物。Dir は sitate ユーザーの systemd 単位の置き場(HOME の下の contract の systemd_user_dir)。
// Scheduler は "systemd"(既定)か "cron"(選ぶのは DetectScheduler。両方を同時に登録しない)。Schedule は定義の schedule("HH:MM")、TZ は定義の timezone。
// 空なら State の定義から読み、定義が無ければ既定(02:30・Asia/Tokyo。spec 4.1)。外側の上限は SetOuterLimit で決める(既定は contract.TimerSystemdTimeoutStart)。
// Run が nil のときは単位を書くだけで有効化しない(部品の試験用)。State があれば有効化の後に state.json の timers を書く。
type Timers struct {
	Dir       string
	Scheduler string
	Schedule  string
	TZ        string
	State     contract.StateFiles
	// Run は外部コマンドを実行する(env は追加の環境変数、stdin は標準入力)。
	Run func(ctx context.Context, env []string, stdin string, name string, args ...string) error
	// RuntimeDir は XDG_RUNTIME_DIR が無いときに与える値(既定 /run/user/<uid>)。
	RuntimeDir string

	limit time.Duration
}

var _ contract.Timers = (*Timers)(nil)

var periods = map[string]bool{"hourly": true, "daily": true, "weekly": true, "monthly": true}

const (
	defaultSchedule = "02:30"
	defaultTZ       = "Asia/Tokyo"
)

// SetOuterLimit は contract.Timers を満たす。上限 + 猶予 + 記録の締め切り(正であること)。登録はこの値で描画する。
func (t *Timers) SetOuterLimit(_ context.Context, limit time.Duration) error {
	if limit <= 0 {
		return newErr(contract.CodeArgInvalid, map[string]any{"flag": "--limit", "reason": "format"})
	}
	t.limit = limit
	return nil
}

// DetectScheduler は登録先を選ぶ(契約の scheduler_choice): runtime(既定 /run/systemd/system)があり systemctl が使えれば "systemd"、でなければ "cron"。
func DetectScheduler(runtime string) string {
	if runtime == "" {
		runtime = "/run/systemd/system"
	}
	if _, err := os.Stat(runtime); err == nil {
		if _, err := exec.LookPath("systemctl"); err == nil {
			return "systemd"
		}
	}
	return "cron"
}

func (t *Timers) backend() string {
	if t.Scheduler == "cron" {
		return "cron"
	}
	return "systemd"
}

// defSchedule は定義の schedule・timezone(引数が空のとき)。読めなければ既定。
func (t *Timers) defSchedule(ctx context.Context) (string, string) {
	sched, tz := t.Schedule, t.TZ
	if (sched == "" || tz == "") && t.State != nil {
		if norm, _, err := t.State.ReadDef(ctx); err == nil {
			var d struct {
				Schedule string `json:"schedule"`
				Timezone string `json:"timezone"`
			}
			_ = json.Unmarshal(norm, &d)
			if sched == "" {
				sched = d.Schedule
			}
			if tz == "" {
				tz = d.Timezone
			}
		}
	}
	if sched == "" {
		sched = defaultSchedule
	}
	if tz == "" {
		tz = defaultTZ
	}
	return sched, tz
}

// Register は contract.Timers を満たす。周期が hourly/daily/weekly/monthly 以外の単位は拒否する(何も書かない)。
// 表の全単位を描画して書き、有効にする(systemd は sitate の --user の単位を daemon-reload して enable --now、cron は crontab -)。
// state.json の timers は有効化に成功した後で最後に書く(失敗したら書かない)。
func (t *Timers) Register(ctx context.Context, units []contract.TimerUnit) error {
	for _, u := range units {
		if len(u.Words) == 0 || !periods[u.Period] {
			return newErr(contract.CodeArgInvalid, map[string]any{"flag": "--period", "reason": "format"})
		}
	}
	tab, err := contract.LoadTimerTable()
	if err != nil {
		return internalErr()
	}
	sched, tz := t.defSchedule(ctx)
	files, err := t.render(tab, sched, tz)
	if err != nil {
		return internalErr()
	}
	be := t.backend()
	if be == "cron" {
		if t.Run != nil {
			if err := t.Run(ctx, nil, files["cron"], tab.CronInstall, "-"); err != nil {
				return internalErr()
			}
		}
		return t.recordBackend(ctx, be)
	}
	if err := os.MkdirAll(t.Dir, 0o755); err != nil {
		return internalErr()
	}
	var timerNames []string
	for name, body := range files {
		if name == "cron" {
			continue
		}
		if err := os.WriteFile(filepath.Join(t.Dir, name), []byte(body), 0o644); err != nil {
			return internalErr()
		}
		if strings.HasSuffix(name, ".timer") {
			timerNames = append(timerNames, name)
		}
	}
	sort.Strings(timerNames)
	if t.Run != nil {
		if err := t.activate(ctx, timerNames); err != nil {
			return internalErr()
		}
	}
	return t.recordBackend(ctx, be)
}

func (t *Timers) activate(ctx context.Context, timers []string) error {
	env := []string{}
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		rd := t.RuntimeDir
		if rd == "" {
			rd = fmt.Sprintf("/run/user/%d", os.Getuid())
		}
		env = append(env, "XDG_RUNTIME_DIR="+rd)
	}
	if err := t.Run(ctx, env, "", "systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	for _, n := range timers {
		if err := t.Run(ctx, env, "", "systemctl", "--user", "enable", "--now", n); err != nil {
			return err
		}
	}
	return nil
}

func (t *Timers) recordBackend(ctx context.Context, be string) error {
	if t.State == nil {
		return nil
	}
	h, err := t.State.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	v, _ := json.Marshal(map[string]string{"backend": be})
	if err := t.State.Update(ctx, h, "timers", v); err != nil {
		return internalErr()
	}
	return nil
}

func hhmm(s string) (int, int, error) {
	tm, err := time.Parse("15:04", s)
	return tm.Hour(), tm.Minute(), err
}

func cmdLine(tab contract.TimerDefTable, args []string) string {
	return strings.Join(append(append([]string{tab.Binary}, args...), tab.InvokedByArgs...), " ")
}

func calendar(u contract.TimerDefUnit, h, m int, tz string) string {
	uh, um, _ := hhmm(u.Time)
	switch u.Trigger {
	case "daily":
		return fmt.Sprintf("*-*-* %02d:%02d:00 %s", h, m, tz)
	case "hourly":
		return fmt.Sprintf("*-*-* *:%02d:00 %s", (m+u.MinuteOffset)%60, tz)
	case "weekly":
		return fmt.Sprintf("%s *-*-* %02d:%02d:00 %s", u.Weekday, uh, um, tz)
	}
	return fmt.Sprintf("*-*-01 %02d:%02d:00 %s", uh, um, tz)
}

func cronSpec(u contract.TimerDefUnit, h, m int) string {
	uh, um, _ := hhmm(u.Time)
	switch u.Trigger {
	case "daily":
		return fmt.Sprintf("%d %d * * *", m, h)
	case "hourly":
		return fmt.Sprintf("%d * * * *", (m+u.MinuteOffset)%60)
	case "weekly":
		days := map[string]int{"Sun": 0, "Mon": 1, "Tue": 2, "Wed": 3, "Thu": 4, "Fri": 5, "Sat": 6}
		return fmt.Sprintf("%d %d * * %d", um, uh, days[u.Weekday])
	}
	return fmt.Sprintf("%d %d 1 * *", um, uh)
}

func renderTmpl(name string, data any) (string, error) {
	src, err := contract.TimerTemplate(name)
	if err != nil {
		return "", err
	}
	tp, err := template.New(name).Parse(src)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	err = tp.Execute(&b, data)
	return b.String(), err
}

// render はファイル名(systemd は <unit_prefix><name>.service / .timer、cron は "cron")から内容を作る。
// systemd は TimeoutStartSec= に外側の上限、cron は `timeout -k <記録の締め切り> <上限 - 記録の締め切り>` で包む(規則 12)。
func (t *Timers) render(tab contract.TimerDefTable, sched, tz string) (map[string]string, error) {
	h, m, err := hhmm(sched)
	if err != nil {
		return nil, err
	}
	limit := t.limit
	if limit <= 0 {
		limit = contract.TimerSystemdTimeoutStart
	}
	kill := int(contract.TimerCronKillAfter / time.Second)
	term := int((limit - contract.TimerCronKillAfter) / time.Second)
	out := map[string]string{}
	var cron []string
	for _, u := range tab.Units {
		name := tab.UnitPrefix + u.Name
		var steps []map[string]string
		var cronSteps, retake []string
		for _, s := range u.Steps {
			c := cmdLine(tab, s.Args)
			sd, cr := c, fmt.Sprintf("timeout -k %d %d %s", kill, term, c)
			if s.When == "sunday" {
				sd = fmt.Sprintf(`/bin/sh -c 'if [ "$$(date +%%%%u)" = 7 ]; then exec %s; fi'`, c)
				cr = fmt.Sprintf(`if [ "$(date +\%%u)" = 7 ]; then %s; fi`, cr)
			}
			steps = append(steps, map[string]string{"Systemd": sd})
			cronSteps = append(cronSteps, cr)
			if s.Retake {
				retake = append(retake, fmt.Sprintf("timeout -k %d %d %s", kill, term, c))
			}
		}
		svc, err := renderTmpl("systemd.service.tmpl", map[string]any{"Name": u.Name, "TZ": tz, "TimeoutStartSec": int(limit / time.Second), "Steps": steps})
		if err != nil {
			return nil, err
		}
		tm, err := renderTmpl("systemd.timer.tmpl", map[string]any{"Name": u.Name, "OnCalendar": calendar(u, h, m, tz), "Persistent": u.Persistent, "UnitName": name})
		if err != nil {
			return nil, err
		}
		out[name+".service"], out[name+".timer"] = svc, tm
		cron = append(cron, fmt.Sprintf("%s /bin/sh -c '%s'", cronSpec(u, h, m), strings.Join(cronSteps, "; ")))
		if u.Persistent && len(retake) > 0 {
			cron = append(cron, fmt.Sprintf("@reboot /bin/sh -c '%s'", strings.Join(retake, "; ")))
		}
	}
	c, err := renderTmpl("cron.tmpl", map[string]any{"TZ": tz, "Lines": cron})
	out["cron"] = c
	return out, err
}
