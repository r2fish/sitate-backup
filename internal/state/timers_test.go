package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type runRec struct {
	calls []string
	stdin []string
	envs  [][]string
	fail  string
}

func (r *runRec) run(_ context.Context, env []string, stdin, name string, args ...string) error {
	c := name + " " + strings.Join(args, " ")
	r.calls, r.stdin, r.envs = append(r.calls, c), append(r.stdin, stdin), append(r.envs, env)
	if r.fail != "" && strings.Contains(c, r.fail) {
		return errors.New("boom")
	}
	return nil
}

var oneUnit = []contract.TimerUnit{{Words: []string{"backup"}, Period: "daily"}}

func newTimers(t *testing.T, sched string, rr *runRec) (*Timers, *StateFiles) {
	t.Helper()
	sf := &StateFiles{Dir: t.TempDir()}
	return &Timers{Dir: filepath.Join(t.TempDir(), "user"), Scheduler: sched, State: sf, Run: rr.run, RuntimeDir: "/run/user/1234"}, sf
}

func timersItem(t *testing.T, sf *StateFiles) string {
	t.Helper()
	v, err := sf.Get(context.Background(), "timers")
	if err != nil {
		t.Fatal(err)
	}
	return string(v)
}

func TestTimersSystemdUserEnable(t *testing.T) {
	rr := &runRec{}
	tm, sf := newTimers(t, "systemd", rr)
	if err := tm.Register(context.Background(), oneUnit); err != nil {
		t.Fatal(err)
	}
	if rr.calls[0] != "systemctl --user daemon-reload" {
		t.Errorf("最初は daemon-reload: %v", rr.calls)
	}
	n := 0
	for _, c := range rr.calls[1:] {
		if !strings.HasPrefix(c, "systemctl --user enable --now sitate-") || !strings.HasSuffix(c, ".timer") {
			t.Errorf("有効化の行: %q", c)
		}
		n++
	}
	if n != 4 {
		t.Errorf("timer の単位は 4 つ: %d", n)
	}
	if len(rr.envs[0]) != 1 || rr.envs[0][0] != "XDG_RUNTIME_DIR=/run/user/1234" {
		if os.Getenv("XDG_RUNTIME_DIR") == "" {
			t.Errorf("XDG_RUNTIME_DIR を与える: %v", rr.envs[0])
		}
	}
	svc, _ := os.ReadFile(filepath.Join(tm.Dir, "sitate-daily.service"))
	if strings.Contains(string(svc), "User=") {
		t.Error("user の単位に User= を書かない")
	}
	timer, _ := os.ReadFile(filepath.Join(tm.Dir, "sitate-daily.timer"))
	if !strings.Contains(string(timer), "OnCalendar=*-*-* 02:30:00 Asia/Tokyo") {
		t.Errorf("既定の 02:30 Asia/Tokyo: %s", timer)
	}
	if got := timersItem(t, sf); got != `{"backend":"systemd"}` {
		t.Errorf("state.json の timers: %s", got)
	}
}

func TestTimersEnableFailureLeavesStateUntouched(t *testing.T) {
	rr := &runRec{fail: "enable --now"}
	tm, sf := newTimers(t, "systemd", rr)
	if err := tm.Register(context.Background(), oneUnit); err == nil {
		t.Fatal("有効化の失敗が成功になった")
	}
	if got := timersItem(t, sf); got != "null" && got != "" {
		t.Errorf("失敗したのに timers が書かれた: %s", got)
	}
	rr = &runRec{fail: "daemon-reload"}
	tm, sf = newTimers(t, "systemd", rr)
	if err := tm.Register(context.Background(), oneUnit); err == nil {
		t.Fatal("daemon-reload の失敗が成功になった")
	}
	if got := timersItem(t, sf); got != "null" && got != "" {
		t.Errorf("失敗したのに timers が書かれた: %s", got)
	}
}

func TestTimersCronInstall(t *testing.T) {
	rr := &runRec{}
	tm, sf := newTimers(t, "cron", rr)
	tm.Schedule, tm.TZ = "03:15", "UTC"
	if err := tm.Register(context.Background(), oneUnit); err != nil {
		t.Fatal(err)
	}
	if len(rr.calls) != 1 || rr.calls[0] != "crontab -" {
		t.Fatalf("crontab - で入れる: %v", rr.calls)
	}
	body := rr.stdin[0]
	if !strings.Contains(body, "15 3 * * * /bin/sh -c 'timeout -k 60 ") || strings.Contains(body, " sitate /bin/sh") {
		t.Errorf("利用者の欄が無い 5 欄 + コマンド: %s", body)
	}
	if !strings.Contains(body, "CRON_TZ=UTC") || !strings.Contains(body, "@reboot /bin/sh -c ") {
		t.Errorf("CRON_TZ と @reboot: %s", body)
	}
	if got := timersItem(t, sf); got != `{"backend":"cron"}` {
		t.Errorf("state.json の timers: %s", got)
	}
	if _, err := os.Stat(tm.Dir); err == nil {
		t.Error("cron のとき systemd の単位を書かない")
	}
	rr.fail = "crontab"
	tm2, sf2 := newTimers(t, "cron", rr)
	if err := tm2.Register(context.Background(), oneUnit); err == nil {
		t.Fatal("crontab の失敗が成功になった")
	}
	if got := timersItem(t, sf2); got != "null" && got != "" {
		t.Errorf("失敗したのに timers が書かれた: %s", got)
	}
}

func TestDetectScheduler(t *testing.T) {
	if DetectScheduler(filepath.Join(t.TempDir(), "none")) != "cron" {
		t.Error("systemd が動いていなければ cron")
	}
	if _, err := exec.LookPath("systemctl"); err == nil && DetectScheduler(t.TempDir()) != "systemd" {
		t.Error("systemd が動いていて systemctl があれば systemd")
	}
}

// 定義の schedule・timezone が効く(Timers.Schedule が空のとき State の承認済みの定義から読む)。Register を繰り返しても値が固定されない。
func TestTimersUseApprovedDefinitionSchedule(t *testing.T) {
	rr := &runRec{}
	tm, sf := newTimers(t, "systemd", rr)
	def := `{"version":1,"schedule":"04:45","timezone":"UTC","source":{"host":"app01.example.internal","targets":[{"id":"t1","files":"/srv/app/data"}]},` +
		`"storage":[{"endpoint":"https://s3.example-provider.com","bucket":"example-sitate","key_ref":"storage"}],` +
		`"restore":{"dest":{"at":"ssh://restore@203.0.113.10:22","key_ref":"restore-init"}},"notify":{"to":["ops@example.jp"]}}`
	_ = os.WriteFile(filepath.Join(sf.Dir, "state.json"), []byte(`{"initialized":true}`), 0o600)
	norm, _, _, err := sf.ValidateDef(context.Background(), []byte(def), true)
	if err != nil {
		t.Fatalf("定義の組み立て: %v", err)
	}
	if err := sf.SwitchDef(context.Background(), contract.SwitchInput{Normalized: norm}); err != nil {
		t.Fatalf("定義の切り替え: %v", err)
	}
	if err := tm.Register(context.Background(), oneUnit); err != nil {
		t.Fatal(err)
	}
	timer, _ := os.ReadFile(filepath.Join(tm.Dir, "sitate-daily.timer"))
	if !strings.Contains(string(timer), "OnCalendar=*-*-* 04:45:00 UTC") {
		t.Errorf("定義の schedule: %s", timer)
	}
	if tm.Schedule != "" || tm.TZ != "" {
		t.Error("Register が Schedule・TZ を固定した")
	}
}

// 完了条件 5 の 15 (規則 12): systemd は TimeoutStartSec=(上限 + 猶予 + 記録の締め切り)、cron は timeout -k、各単位の起動の引数に --invoked-by timer。
func TestTimersOuterLimitAndInvokedBy(t *testing.T) {
	limit := 9 * time.Hour
	rr := &runRec{}
	tm, _ := newTimers(t, "systemd", rr)
	if err := tm.SetOuterLimit(context.Background(), limit); err != nil {
		t.Fatal(err)
	}
	if err := tm.Register(context.Background(), oneUnit); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(tm.Dir)
	n := 0
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".service") {
			continue
		}
		n++
		b, _ := os.ReadFile(filepath.Join(tm.Dir, e.Name()))
		if !strings.Contains(string(b), "TimeoutStartSec=32400\n") {
			t.Errorf("%s: TimeoutStartSec: %s", e.Name(), b)
		}
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ln, "ExecStart=") && !strings.Contains(ln, "--invoked-by timer") {
				t.Errorf("%s: --invoked-by timer が無い: %s", e.Name(), ln)
			}
		}
	}
	if n != 4 {
		t.Errorf("service 単位: %d", n)
	}
	rc := &runRec{}
	tc, _ := newTimers(t, "cron", rc)
	_ = tc.SetOuterLimit(context.Background(), limit)
	if err := tc.Register(context.Background(), oneUnit); err != nil {
		t.Fatal(err)
	}
	kill := int(contract.TimerCronKillAfter / time.Second)
	want := fmt.Sprintf("timeout -k %d %d ", kill, int((limit-contract.TimerCronKillAfter)/time.Second))
	for _, ln := range strings.Split(rc.stdin[0], "\n") {
		if strings.Contains(ln, "/usr/local/bin/sitate") {
			if !strings.Contains(ln, want) || !strings.Contains(ln, "--invoked-by timer") {
				t.Errorf("cron の行: %s (want %q)", ln, want)
			}
		}
	}
}
