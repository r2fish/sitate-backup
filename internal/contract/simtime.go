package contract

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// EnvSimTimeFile は模擬時間の共有の値(試験環境の中の 1 ファイル)を指す環境変数の名前(作業指示書 3.3)。
// 試験ビルドの核(時計のオフセット)、test-tick、偽の保管先(Go パッケージとサーバー)が、時刻を読むたびにこのファイルを読む。
// 製品ビルドはこの値を読まない。
const EnvSimTimeFile = "SITATE_SIM_TIME_FILE"

// SimTimeFormat は共有の値の format の値。
const SimTimeFormat = "sitate.simtime/v1"

// SimTime は共有の値の形(JSON 1 件)。模擬の「今」は SimNow + (実時刻 - RealAt)、
// 時計のオフセットは SimNow - RealAt(書いたときに固定する)。時刻は UTC の RFC 3339 で秒まで。
type SimTime struct {
	Format string `json:"format"`
	SimNow string `json:"sim_now"`
	RealAt string `json:"real_at"`
}

// ErrSimTimeInvalid は共有の値が無い・壊れているときの誤り。試験ビルドの核は終了 2(ARG_INVALID、detail.reason: format)、
// 偽の保管先は内部エラー(S3 の 5xx 相当)で止まる。
var ErrSimTimeInvalid = errors.New("simtime: 共有の値が無いか壊れている")

// ReadSimTime は path の共有の値を読む。読むたびに呼ぶ(起動時の値に固定しない)。
func ReadSimTime(path string) (SimTime, error) {
	var s SimTime
	b, err := os.ReadFile(path)
	if err != nil {
		return s, fmt.Errorf("%w: %v", ErrSimTimeInvalid, err)
	}
	if err := json.Unmarshal(b, &s); err != nil || s.Format != SimTimeFormat {
		return s, fmt.Errorf("%w: 形が違う", ErrSimTimeInvalid)
	}
	if _, err := s.Offset(); err != nil {
		return s, err
	}
	return s, nil
}

// Offset は SimNow - RealAt を返す。
func (s SimTime) Offset() (time.Duration, error) {
	a, e1 := time.Parse(time.RFC3339, s.SimNow)
	r, e2 := time.Parse(time.RFC3339, s.RealAt)
	if e1 != nil || e2 != nil {
		return 0, fmt.Errorf("%w: 時刻の形", ErrSimTimeInvalid)
	}
	return a.Sub(r), nil
}

// Now は模擬の今を返す。
func (s SimTime) Now(real time.Time) (time.Time, error) {
	off, err := s.Offset()
	if err != nil {
		return time.Time{}, err
	}
	return real.Add(off).UTC().Truncate(time.Second), nil
}

// WriteSimTime は共有の値を原子的に書く(試験と test-tick が使う)。
func WriteSimTime(path string, simNow, realAt time.Time) error {
	b, _ := json.Marshal(SimTime{Format: SimTimeFormat, SimNow: simNow.UTC().Format(time.RFC3339), RealAt: realAt.UTC().Format(time.RFC3339)})
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// TestTickStart は test-tick の模擬時間の開始日時(作業指示書 3.3。月曜日かつ 31 日の月の 1 日の 00:00)。
// 定義の timezone で解釈する。2027-03-01 は月曜日で、3 月は 31 日。init の前は timezone が分からないため、台本が test-tick --set-start の値として RFC 3339 で渡す。
const TestTickStart = "2027-03-01T00:00:00"
