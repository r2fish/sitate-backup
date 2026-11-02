//go:build sitate_testbuild

package cli

// `test-tick --days N`(作業指示書 3.3 の「test-tick の 1 模擬日」): タイマー列(contract の timers.json)を模擬時間で 1 日ずつ進めながら、
// サブコマンドごとに子プロセスで起動する。引数は Timers の登録と同じ(末尾に --invoked-by timer)。

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type tickEvent struct {
	at    time.Time
	steps []contract.TimerDefStep
}

// tickEvents は 1 模擬日(dayStart から 24 時間)の起動を時刻の順に返す。
func tickEvents(tab contract.TimerDefTable, dayStart time.Time, schedH, schedM int) []tickEvent {
	at := func(h, m int) time.Time {
		return time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), h, m, 0, 0, dayStart.Location())
	}
	var evs []tickEvent
	for _, u := range tab.Units {
		var times []time.Time
		switch u.Trigger {
		case "daily":
			times = append(times, at(schedH, schedM))
		case "hourly":
			for h := 0; h < 24; h++ {
				times = append(times, at(h, (schedM+u.MinuteOffset)%60))
			}
		case "weekly":
			if dayStart.Weekday().String()[:3] == u.Weekday {
				times = append(times, parseHM(at, u.Time))
			}
		case "monthly":
			if dayStart.Day() == 1 {
				times = append(times, parseHM(at, u.Time))
			}
		}
		for _, t := range times {
			var steps []contract.TimerDefStep
			for _, s := range u.Steps {
				if s.When == "always" || (s.When == "sunday" && dayStart.Weekday() == time.Sunday) {
					steps = append(steps, s)
				}
			}
			evs = append(evs, tickEvent{t, steps})
		}
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].at.Before(evs[j].at) })
	return evs
}

func parseHM(at func(h, m int) time.Time, s string) time.Time {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return at(0, 0)
	}
	return at(t.Hour(), t.Minute())
}

// scheduleOf は定義の schedule と timezone(書かれていなければ既定 02:30 と Asia/Tokyo)。定義が読めないときも既定。
func (d *Deps) scheduleOf(ctx context.Context) (h, m int, loc *time.Location) {
	sched, tz := "02:30", "Asia/Tokyo"
	if d.State != nil {
		norm, _, err := d.State.ReadDef(ctx)
		if err != nil {
			norm, _, _, _ = d.State.ReadCandidate(ctx)
		}
		var def struct {
			Schedule string `json:"schedule"`
			Timezone string `json:"timezone"`
		}
		if json.Unmarshal(norm, &def) == nil {
			if def.Schedule != "" {
				sched = def.Schedule
			}
			if def.Timezone != "" {
				tz = def.Timezone
			}
		}
	}
	t, _ := time.Parse("15:04", sched)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return t.Hour(), t.Minute(), loc
}

func tickDays(ctx context.Context, d *Deps, path string, n int) (contract.Reply, error) {
	tab, err := contract.LoadTimerTable()
	if err != nil {
		return contract.Reply{}, internalErr()
	}
	now, err := d.Clock.Now(ctx)
	if err != nil {
		return contract.Reply{}, toError(err)
	}
	h, m, loc := d.scheduleOf(ctx)
	start := now.In(loc)
	for k := 0; k < n; k++ {
		day := start.AddDate(0, 0, k)
		for _, ev := range tickEvents(tab, day, h, m) {
			if ev.at.Before(now) {
				continue
			}
			for _, s := range ev.steps {
				if err := contract.WriteSimTime(path, ev.at, time.Now()); err != nil {
					return contract.Reply{}, internalErr()
				}
				argv := append(append([]string{}, s.Args...), tab.InvokedByArgs...)
				if _, err := d.spawnFn()(ctx, argv); err != nil {
					return contract.Reply{}, toError(err)
				}
			}
		}
	}
	end := start.AddDate(0, 0, n)
	if err := contract.WriteSimTime(path, end, time.Now()); err != nil {
		return contract.Reply{}, internalErr()
	}
	b, _ := json.Marshal(map[string]any{"days": n, "now": end.UTC().Format(time.RFC3339)})
	return contract.Reply{Result: b}, nil
}

// spawnFn は子プロセスの起動口(既定は自分自身の実行ファイル)。試験が差し替えて起動の引数を記録する。
func (d *Deps) spawnFn() func(context.Context, []string) (int, error) {
	if d.spawn != nil {
		return d.spawn
	}
	return func(ctx context.Context, argv []string) (int, error) {
		exe, err := os.Executable()
		if err != nil {
			return 0, internalErr()
		}
		cmd := exec.CommandContext(ctx, exe, argv...)
		cmd.Stdout, cmd.Stderr, cmd.Env = io.Discard, io.Discard, os.Environ()
		childAttr(cmd)
		err = cmd.Run()
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		if err != nil {
			return 0, internalErr()
		}
		return 0, nil
	}
}
