package approval

// pending/log.jsonl: 事象の追記のみの記録とハッシュ連鎖(spec 4.5、4.6)。

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/gowebpki/jcs"
	"github.com/zeebo/blake3"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 事象(4.6 の event の値)。
const (
	evProposed  = "proposed"
	evApproved  = "approved"
	evApplying  = "applying"
	evApplied   = "applied"
	evCancelled = "cancelled"
	evExpired   = "expired"
	evFailed    = "failed"
	evCommitted = "committed"
)

// 連鎖の切れの種類(4.6)。
const (
	chainGap          = "gap"
	chainFork         = "fork"
	chainPrevMismatch = "prev_mismatch"
	chainMalformed    = "malformed"
)

type logEvent struct {
	Seq      int             `json:"seq"`
	PrevHash *string         `json:"prev_hash"`
	Event    string          `json:"event"`
	ChangeID string          `json:"change_id"`
	Instance string          `json:"instance,omitempty"`
	Code     string          `json:"code,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
}

type chainErr struct {
	kind string
	seq  int // 最初に切れていた行の seq(読めなければ -1 = null)
}

func (c *chainErr) toError(discarded []string) *contract.Error {
	if discarded == nil {
		discarded = []string{}
	}
	return apiErr(contract.CodePendingChainBroken, map[string]any{"kind": c.kind, "seq": c.seqValue(), "discarded": discarded})
}

func (c *chainErr) seqValue() any {
	if c.seq < 0 {
		return nil
	}
	return c.seq
}

func lineHash(line []byte) string {
	h := blake3.Sum256(line)
	return hex.EncodeToString(h[:])
}

var knownEvents = map[string]bool{evProposed: true, evApproved: true, evApplying: true, evApplied: true, evCancelled: true, evExpired: true, evFailed: true, evCommitted: true}

type pstore struct{ b blobs }

// rawLog は log.jsonl の完結した行(改行を除く)と、書きかけの最後の行があったか。
func (s *pstore) rawLog() (lines [][]byte, partial bool, err error) {
	b, _, err := s.b.read(logName)
	if err != nil || len(b) == 0 {
		return nil, false, err
	}
	parts := bytes.Split(b, []byte("\n"))
	last := parts[len(parts)-1]
	parts = parts[:len(parts)-1]
	return parts, len(last) > 0, nil
}

// events は連鎖を確かめて事象を返す。切れていれば chainErr を返す。
func (s *pstore) events() ([]logEvent, [][]byte, *chainErr, error) {
	lines, _, err := s.rawLog()
	if err != nil {
		return nil, nil, nil, err
	}
	evs := make([]logEvent, 0, len(lines))
	for i, ln := range lines {
		var ev logEvent
		if json.Unmarshal(ln, &ev) != nil || ev.ChangeID == "" || !knownEvents[ev.Event] {
			return nil, lines, &chainErr{chainMalformed, -1}, nil
		}
		if ev.Seq < i+1 {
			return nil, lines, &chainErr{chainFork, ev.Seq}, nil
		}
		if ev.Seq > i+1 {
			return nil, lines, &chainErr{chainGap, ev.Seq}, nil
		}
		if !prevOK(ev.PrevHash, lines, i) {
			return nil, lines, &chainErr{chainPrevMismatch, ev.Seq}, nil
		}
		evs = append(evs, ev)
	}
	return evs, lines, nil, nil
}

func prevOK(p *string, lines [][]byte, i int) bool {
	if i == 0 {
		return p == nil
	}
	return p != nil && *p == lineHash(lines[i-1])
}

// appendEvent は事象を 1 行追記する。呼び出し側が state.lock を持ち、直前に events() で連鎖を確かめている。
// 遷移の規則(4.6)に反する追記は CHANGE_STATE_INVALID にする(cancelled の後の applying など)。
func (s *pstore) appendEvent(ev logEvent) (logEvent, error) {
	evs, lines, ce, err := s.events()
	if err != nil {
		return ev, err
	}
	if ce != nil {
		return ev, ce.toError(nil)
	}
	if last := lastEventOf(evs, ev.ChangeID); !transitionOK(last, ev.Event) {
		var st *string
		if last != "" {
			st = &last
		}
		return ev, stateErr(ev.ChangeID, st)
	}
	ev.Seq = len(lines) + 1
	ev.PrevHash = nil
	if len(lines) > 0 {
		h := lineHash(lines[len(lines)-1])
		ev.PrevHash = &h
	}
	line, err := encodeLine(ev)
	if err != nil {
		return ev, err
	}
	if _, partial, _ := s.rawLog(); partial { // 書きかけの最後の行は次の追記の前に切り詰める
		if err := s.b.write(logName, joinLines(lines)); err != nil {
			return ev, err
		}
	}
	return ev, s.b.appendTo(logName, line)
}

func joinLines(lines [][]byte) []byte {
	var out []byte
	for _, l := range lines {
		out = append(append(out, l...), '\n')
	}
	return out
}

func encodeLine(ev logEvent) ([]byte, error) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	c, err := jcs.Transform(raw)
	if err != nil {
		return nil, fmt.Errorf("approval: log line: %w", err)
	}
	return append(c, '\n'), nil
}

func lastEventOf(evs []logEvent, id string) string {
	last := ""
	for _, e := range evs {
		if e.ChangeID == id {
			last = e.Event
		}
	}
	return last
}

// transitionOK は状態の遷移(4.6): proposed → approved → applying → applied(→ committed)、または cancelled(approved まで)/ expired / failed。
func transitionOK(from, to string) bool {
	switch to {
	case evProposed:
		return from == ""
	case evApproved:
		return from == evProposed
	case evApplying:
		return from == evApproved
	case evApplied:
		return from == evApplying
	case evCommitted:
		return from == evApplied
	case evCancelled:
		return from == evProposed || from == evApproved
	case evExpired:
		return from == evProposed || from == evApproved
	case evFailed:
		return from == evApproved || from == evApplying
	}
	return false
}

// chgState は事象の列から作る変更ごとの状態。
type chgState struct {
	ID       string
	Last     string
	Instance string // 最後の applying の instance
	Code     string // 最後の failed の code
	Result   json.RawMessage
}

// replay は変更の最初の出現の順と、変更ごとの状態を返す。
func replay(evs []logEvent) ([]string, map[string]*chgState) {
	var order []string
	m := map[string]*chgState{}
	for _, e := range evs {
		st := m[e.ChangeID]
		if st == nil {
			st = &chgState{ID: e.ChangeID}
			m[e.ChangeID] = st
			order = append(order, e.ChangeID)
		}
		st.Last = e.Event
		switch e.Event {
		case evApplying:
			st.Instance = e.Instance
		case evFailed:
			st.Code = e.Code
		case evApplied:
			st.Result = e.Result
		}
	}
	return order, m
}
