//go:build mainpath

package mainpath

// 第 3 段: 通知の受け手(受入試験の環境の hook)の読み出し。記録ファイルは 1 件 1 行の JSON(欄: seq, dest, status, body, confirmation_word)。

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const hookDest = "ops-hook" // notify.to の Webhook の参照名 = hook の URL のパスの宛先

type hookRec struct {
	Seq              int             `json:"seq"`
	Dest             string          `json:"dest"`
	Status           int             `json:"status"`
	Body             json.RawMessage `json:"body"`
	ConfirmationWord string          `json:"confirmation_word"`
}

func (r hookRec) eventType() string {
	var b struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(r.Body, &b)
	return b.Type
}

func (r hookRec) fact(k string) any {
	var b struct {
		Facts map[string]any `json:"facts"`
	}
	_ = json.Unmarshal(r.Body, &b)
	return b.Facts[k]
}

// hookRecords は宛先 dest の受信の記録を、受けた順に返す。
func hookRecords(t *testing.T, dest string) []hookRec {
	t.Helper()
	b, err := os.ReadFile(accEnv(t, "SITATE_ACCEPT_HOOK_RECORDS_FILE"))
	if os.IsNotExist(err) {
		return nil // 最初の受信まで記録ファイルは無い
	}
	if err != nil {
		t.Fatalf("hook の記録を読めない: %v", err)
	}
	var out []hookRec
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		var r hookRec
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("hook の記録の行が JSON でない: %v", err)
		}
		if r.Dest == dest {
			out = append(out, r)
		}
	}
	return out
}

// hookEvents は宛先 dest で受理された(2xx)イベントのうち type が一致するもの。
func hookEvents(t *testing.T, dest, typ string) []hookRec {
	var out []hookRec
	for _, r := range hookRecords(t, dest) {
		if r.Status/100 == 2 && r.eventType() == typ {
			out = append(out, r)
		}
	}
	return out
}

// lastConfirmationWord は宛先で最後に受けた notify.test の確認語(人が通知で受け取る語の役)。
func lastConfirmationWord(t *testing.T, dest string) string {
	t.Helper()
	ev := hookEvents(t, dest, "notify.test")
	if len(ev) == 0 {
		t.Fatalf("hook(%s)に notify.test が届いていない", dest)
	}
	w, _ := ev[len(ev)-1].fact("confirmation_word").(string)
	if w == "" {
		t.Fatalf("notify.test に facts.confirmation_word が無い")
	}
	return w
}
