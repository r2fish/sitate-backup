package harness

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 定期実行の雛形の参照の描画と、test-tick の周期の数え方(試験ベクトル用)。製品の描画(U1f-1)ではない。

func hhmm(s string) (int, int, error) {
	t, err := time.Parse("15:04", s)
	return t.Hour(), t.Minute(), err
}

func cmdLine(t contract.TimerDefTable, args []string) string {
	return strings.Join(append(append([]string{t.Binary}, args...), t.InvokedByArgs...), " ")
}

func calendar(u contract.TimerDefUnit, h, m int, tz string) string {
	switch u.Trigger {
	case "daily":
		return fmt.Sprintf("*-*-* %02d:%02d:00 %s", h, m, tz)
	case "hourly":
		return fmt.Sprintf("*-*-* *:%02d:00 %s", (m+u.MinuteOffset)%60, tz)
	case "weekly":
		uh, um, _ := hhmm(u.Time)
		return fmt.Sprintf("%s *-*-* %02d:%02d:00 %s", u.Weekday, uh, um, tz)
	}
	uh, um, _ := hhmm(u.Time)
	return fmt.Sprintf("*-*-01 %02d:%02d:00 %s", uh, um, tz)
}

func cronSpec(u contract.TimerDefUnit, h, m int) string {
	switch u.Trigger {
	case "daily":
		return fmt.Sprintf("%d %d * * *", m, h)
	case "hourly":
		return fmt.Sprintf("%d * * * *", (m+u.MinuteOffset)%60)
	case "weekly":
		uh, um, _ := hhmm(u.Time)
		return fmt.Sprintf("%d %d * * %d", um, uh, map[string]int{"Sun": 0, "Mon": 1, "Tue": 2, "Wed": 3, "Thu": 4, "Fri": 5, "Sat": 6}[u.Weekday])
	}
	uh, um, _ := hhmm(u.Time)
	return fmt.Sprintf("%d %d 1 * *", um, uh)
}

func render(name string, data any) (string, error) {
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

// RenderTimers は schedule("HH:MM")と timezone から、systemd の単位ファイルと cron の記述を描画する。
// 戻りは ファイル名(systemd は <unit_prefix><name>.service / .timer、cron は cron)→ 内容。
func RenderTimers(t contract.TimerDefTable, schedule, tz string) (map[string]string, error) {
	h, m, err := hhmm(schedule)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	timeout := int(contract.TimerCronTerm / time.Second)
	kill := int(contract.TimerCronKillAfter / time.Second)
	var cron []string
	for _, u := range t.Units {
		name := t.UnitPrefix + u.Name
		var steps []map[string]string
		var cronSteps, retake []string
		for _, s := range u.Steps {
			c := cmdLine(t, s.Args)
			sd, cr := c, fmt.Sprintf("timeout -k %d %d %s", kill, timeout, c)
			if s.When == "sunday" {
				sd = fmt.Sprintf(`/bin/sh -c 'if [ "$$(date +%%%%u)" = 7 ]; then exec %s; fi'`, c)
				cr = fmt.Sprintf(`if [ "$(date +\%%u)" = 7 ]; then %s; fi`, cr)
			}
			steps = append(steps, map[string]string{"Systemd": sd})
			cronSteps = append(cronSteps, cr)
			if s.Retake {
				retake = append(retake, fmt.Sprintf("timeout -k %d %d %s", kill, timeout, c))
			}
		}
		svc, err := render("systemd.service.tmpl", map[string]any{"Name": u.Name, "User": t.ServiceUser, "TZ": tz, "TimeoutStartSec": int(contract.TimerSystemdTimeoutStart / time.Second), "Steps": steps})
		if err != nil {
			return nil, err
		}
		tm, err := render("systemd.timer.tmpl", map[string]any{"Name": u.Name, "OnCalendar": calendar(u, h, m, tz), "Persistent": u.Persistent, "UnitName": name})
		if err != nil {
			return nil, err
		}
		out[name+".service"], out[name+".timer"] = svc, tm
		cron = append(cron, fmt.Sprintf("%s /bin/sh -c '%s'", cronSpec(u, h, m), strings.Join(cronSteps, "; ")))
		if u.Persistent && len(retake) > 0 {
			cron = append(cron, fmt.Sprintf("@reboot /bin/sh -c '%s'", strings.Join(retake, "; ")))
		}
	}
	c, err := render("cron.tmpl", map[string]any{"TZ": tz, "Lines": cron})
	out["cron"] = c
	return out, err
}

// TimerCalls は開始日時(contract.TestTickStart。曜日と日付だけを使う)から days 模擬日の、サブコマンドごとの起動回数を数える。
// キーは引数を空白でつないだもの(例 "report --weekly")。restore-test は起動の回数で、実行の回数は核が決める。
func TimerCalls(t contract.TimerDefTable, days int) map[string]int {
	start, _ := time.Parse("2006-01-02T15:04:05", contract.TestTickStart)
	calls := map[string]int{}
	for k := 0; k < days; k++ {
		d := start.AddDate(0, 0, k)
		for _, u := range t.Units {
			n := 1
			switch u.Trigger {
			case "hourly":
				n = 24
			case "weekly":
				if d.Weekday().String()[:3] != u.Weekday {
					n = 0
				}
			case "monthly":
				if d.Day() != 1 {
					n = 0
				}
			}
			for _, s := range u.Steps {
				if s.When == "sunday" && d.Weekday() != time.Sunday {
					continue
				}
				calls[strings.Join(s.Args, " ")] += n
			}
		}
	}
	return calls
}
