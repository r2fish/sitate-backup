package cli

// 実行中の印 run/marker.json(spec 4.5 の「実行中の印と instance」)の形と読み書き。書くのはロックの持ち主だけ(Lease.WriteMarker)。

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type marker struct {
	RunID         string   `json:"run_id"`
	Command       []string `json:"command"`
	Pid           int      `json:"pid"`
	PidStartTicks int64    `json:"pid_start_ticks"`
	BootID        string   `json:"boot_id"`
	StartedAt     string   `json:"started_at"`
	DeadlineAt    string   `json:"deadline_at"`
}

// markerState は印を読んだ結果。
type markerState int

const (
	markerAbsent     markerState = iota // 印が無い(持ち主がロックを取ってから印を書くまで)
	markerUnreadable                    // JSON として読めない、項目が欠ける
	markerOK
)

func (m marker) valid() bool {
	_, e1 := time.Parse(time.RFC3339, m.StartedAt)
	_, e2 := time.Parse(time.RFC3339, m.DeadlineAt)
	return m.RunID != "" && len(m.Command) > 0 && e1 == nil && e2 == nil
}

func parseMarker(b []byte) (marker, markerState) {
	if len(b) == 0 {
		return marker{}, markerAbsent
	}
	var m marker
	if json.Unmarshal(b, &m) != nil || !m.valid() {
		return marker{}, markerUnreadable
	}
	return m, markerOK
}

func markerPath(dir string) string { return filepath.Join(dir, "run", "marker.json") }

// readMarker は印を読む(ロックの持ち主でなくてよい)。
func readMarker(dir string) (marker, markerState) {
	b, err := os.ReadFile(markerPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return marker{}, markerAbsent
	}
	if err != nil {
		return marker{}, markerUnreadable
	}
	return parseMarker(b)
}

// removeMarker は核が制御して終わるときに印を消す(ロックの持ち主だけ)。run/ の fsync も行う。
func removeMarker(dir string) {
	_ = os.Remove(markerPath(dir))
	if f, err := os.Open(filepath.Join(dir, "run")); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
}

// selfProc は自プロセスの pid、/proc/<pid>/stat の 22 番目の欄、boot_id(Linux 以外では 0 と空)。
func selfProc() (pid int, ticks int64, bootID string) {
	pid = os.Getpid()
	if b, err := os.ReadFile("/proc/self/stat"); err == nil {
		s := string(b)
		if i := strings.LastIndexByte(s, ')'); i >= 0 {
			// ')' の後ろは 3 番目の欄(state)から始まる。22 番目は 20 番目の語。
			if f := strings.Fields(s[i+1:]); len(f) >= 20 {
				ticks, _ = strconv.ParseInt(f[19], 10, 64)
			}
		}
	}
	if b, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
		bootID = strings.ToLower(strings.TrimSpace(string(b)))
	}
	return
}
