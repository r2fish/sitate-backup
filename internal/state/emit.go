package state

// 通知イベントの発行(spec 4.8 の「発行と配送」「まとめ」)。イベントの作成、まとめの判定(state.json の notify.merge[])、
// 実行の記録の notifications[] への記入までを行い、配送は Notifier に渡す。配送の間は state.lock を持たない。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const mergeWindow = 24 * time.Hour

type mergeEntry struct {
	Type            string  `json:"type"`
	Key             *string `json:"key"`
	LastAcceptedAt  *string `json:"last_accepted_at"`
	SuppressedCount int     `json:"suppressed_count"`
}

func eventType(typ string) (contract.EventType, bool) {
	for _, t := range contract.EventTypes {
		if t.Type == typ {
			return t, true
		}
	}
	return contract.EventType{}, false
}

// severityOf は種類の重さを決める。change.failed だけは error_code で warn / critical が分かれる(4.8)。
func severityOf(t contract.EventType, facts map[string]json.RawMessage) string {
	if len(t.Severities) == 1 {
		return t.Severities[0]
	}
	var code string
	_ = json.Unmarshal(facts["error_code"], &code)
	if code == "SIGNATURE_INVALID" || code == "PRECONDITION_CHANGED" {
		return "critical"
	}
	return "warn"
}

// mergeKey はまとめのキー(facts の値を '\x1f' でつないだ文字列)。キーの列が "type" のとき、または空のときは nil(type だけで比べる)。
func mergeKey(t contract.EventType, facts map[string]json.RawMessage) *string {
	if t.MergeKey == "" || t.MergeKey == "type" {
		return nil
	}
	var parts []string
	for _, k := range strings.Split(t.MergeKey, ",") {
		var s string
		if json.Unmarshal(facts[k], &s) != nil {
			s = string(facts[k])
		}
		parts = append(parts, s)
	}
	out := strings.Join(parts, "\x1f")
	return &out
}

// merges はまとめの対象か。warn と、critical の run.stuck(持ち主ごとに 24 時間に 1 回)。
func merges(t contract.EventType, sev string) bool {
	return sev == "warn" || t.Type == "run.stuck"
}

var nextByType = map[string][]string{
	"journal.chain_broken": {"sitate", "journal", "verify"}, "kit.outdated": {"sitate", "kit"}, "phrase.unconfirmed": {"sitate", "phrase", "verify"},
	"kit.location_check_due": nil, "notify.test": nil, "cost.ceiling_exceeded": {"sitate", "estimate"}, "source.uncovered": {"sitate", "source", "check"},
	"storage.permission_drift": {"sitate", "bucket", "check"}, "time.skew": {"sitate", "doctor"}, "verify.failed": {"sitate", "status"},
	"change.proposed": {"sitate", "pending"}, "change.approved": {"sitate", "pending"}, "change.applying_soon": {"sitate", "pending"},
	"change.applied": {"sitate", "pending"}, "change.expired": {"sitate", "pending"}, "hold.set": {"sitate", "status"},
}

// nextMulti は spec 4.8 の表で next が 2 件以上の種類。
var nextMulti = map[string][][]string{
	"verify.failed":              {{"sitate", "status"}, {"sitate", "bucket", "check"}},
	"storage.unexpected_version": {{"sitate", "status"}, {"sitate", "bucket", "check"}},
	"storage.noncurrent_surge":   {{"sitate", "status"}, {"sitate", "bucket", "check"}},
	"storage.size_drift":         {{"sitate", "status"}, {"sitate", "bucket", "check"}},
	"storage.retention_long":     {{"sitate", "status"}, {"sitate", "bucket", "check"}},
	"hold.set":                   {{"sitate", "status"}, {"sitate", "propose", "hold-release"}},
	"kit.location_check_due":     {{"sitate", "kit", "confirm", "--recheck"}},
}

func nextFor(t contract.EventType) []contract.Next {
	if m, ok := nextMulti[t.Type]; ok {
		out := make([]contract.Next, 0, len(m))
		for _, a := range m {
			out = append(out, contract.Next{Argv: a})
		}
		return out
	}
	switch t.Type {
	case "storage.unreachable", "retention.extension_failed", "teardown.unverified", "change.failed":
		return []contract.Next{} // error_code の next(3.1.3)は、原因コードの表に next を持たないため核の組み立てで足す(未実装)
	}
	argv, ok := nextByType[t.Type]
	if !ok {
		argv = []string{"sitate", "status"}
	}
	if argv == nil {
		return []contract.Next{}
	}
	return []contract.Next{{Argv: argv}}
}

// SetRunID は以後のイベントの id を実行の run_id から作る(spec 4.8: e-<run_id の開始時刻と乱数>-<連番>)。連番は 1 から数え直す。
func (r *Recorder) SetRunID(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RunID, r.emitted = runID, 0
}

func (r *Recorder) newEventID(now time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.emitted++
	base := strings.TrimPrefix(r.RunID, "r-")
	if base == "" {
		var b [2]byte
		_, _ = rand.Read(b[:])
		base = now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:])
		r.RunID = "r-" + base
	}
	return fmt.Sprintf("e-%s-%02d", base, r.emitted)
}

// Emit は contract.Recorder を満たす。
func (r *Recorder) Emit(ctx context.Context, typ string, facts []byte) (contract.Event, []contract.DeliveryResult, error) {
	if err := r.ready(); err != nil || r.Notifier == nil {
		return contract.Event{}, nil, internalErr()
	}
	t, ok := eventType(typ)
	fm := map[string]json.RawMessage{}
	if !ok || json.Unmarshal(facts, &fm) != nil || fm == nil {
		return contract.Event{}, nil, newErr(contract.CodeArgInvalid, map[string]any{"flag": "type", "reason": "format"})
	}
	now, err := r.Clock.Now(ctx)
	if err != nil {
		return contract.Event{}, nil, err
	}
	sev := severityOf(t, fm)
	to, repoID, err := r.recipients(ctx)
	if err != nil {
		return contract.Event{}, nil, err
	}
	ev := contract.Event{ID: r.newEventID(now), Type: typ, Facts: facts, Severity: sev, Time: now.UTC(), RepoID: repoID, Next: nextFor(t)}
	key := mergeKey(t, fm)
	if merges(t, sev) {
		suppressed, count, err := r.mergeCheck(ctx, typ, key, now)
		if err != nil {
			return ev, nil, err
		}
		if suppressed {
			r.noteNotification(ev, nil, false)
			return ev, []contract.DeliveryResult{}, nil
		}
		ev.SuppressedCount = count
	}
	res, err := r.Notifier.Deliver(ctx, ev, to)
	if err != nil {
		return ev, nil, err
	}
	r.noteNotification(ev, res, false)
	if merges(t, sev) {
		if err := r.mergeAccepted(ctx, typ, key, res); err != nil {
			return ev, res, err
		}
	}
	return ev, res, nil
}

func (r *Recorder) recipients(ctx context.Context) ([]string, *string, error) {
	if r.Recipients != nil {
		return r.Recipients(ctx)
	}
	var repoID *string
	if v, err := r.State.Get(ctx, "repo_id"); err == nil {
		var s string
		if json.Unmarshal(v, &s) == nil && s != "" {
			repoID = &s
		}
	}
	def, _, err := r.State.ReadDef(ctx)
	if err != nil || len(def) == 0 {
		// seal の前は承認済みの定義が無い。seal の提案・承認の通知(change.proposed・change.approved)を届けるため、候補の定義の宛先を使う
		// (notify test も候補の宛先に送る。確認語で届いたことを確かめる相手と同じ)。候補も無い段階(init の前)は宛先が無い。
		cand, _, found, cerr := r.State.ReadCandidate(ctx)
		if cerr != nil || !found || len(cand) == 0 {
			return nil, repoID, nil
		}
		def = cand
	}
	var d struct {
		Notify struct {
			To []string `json:"to"`
		} `json:"notify"`
	}
	_ = json.Unmarshal(def, &d)
	return d.Notify.To, repoID, nil
}

func (r *Recorder) noteNotification(ev contract.Event, res []contract.DeliveryResult, resend bool) {
	delivered := []map[string]any{}
	for _, d := range res {
		var at any
		if d.AcceptedAt != nil {
			at = d.AcceptedAt.UTC().Format(time.RFC3339)
		}
		var reason any
		if d.Reason != "" {
			reason = d.Reason
		}
		delivered = append(delivered, map[string]any{"to": d.To, "channel": d.Channel, "delivered": d.Delivered, "attempts": d.Attempts, "reason": reason, "accepted_at": at})
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notifications = append(r.notifications, map[string]any{"id": ev.ID, "type": ev.Type, "resend": resend, "delivered": delivered, "abandoned_to": []string{}})
}

// Notifications は発行したイベントの notifications[] の要素(呼び出し側が実行の記録の項目に載せる)。
func (r *Recorder) Notifications() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any{}, r.notifications...)
}

func (r *Recorder) readNotify(ctx context.Context) (map[string]json.RawMessage, []mergeEntry, error) {
	cur, err := r.State.Get(ctx, "notify")
	if err != nil {
		return nil, nil, err
	}
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(cur, &m)
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	var ms []mergeEntry
	_ = json.Unmarshal(m["merge"], &ms)
	return m, ms, nil
}

func sameKey(a, b *string) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

// mergeCheck は同じ種類を 24 時間の内に受理していれば送らず、送らなかった数を足す。24 時間を過ぎていれば数を返して 0 に戻す。
func (r *Recorder) mergeCheck(ctx context.Context, typ string, key *string, now time.Time) (suppressed bool, count int, err error) {
	h, err := r.State.Lock(ctx)
	if err != nil {
		return false, 0, err
	}
	defer h.Release()
	m, ms, err := r.readNotify(ctx)
	if err != nil {
		return false, 0, err
	}
	for i := range ms {
		e := &ms[i]
		if e.Type != typ || !sameKey(e.Key, key) || e.LastAcceptedAt == nil {
			continue
		}
		at, perr := time.Parse(time.RFC3339, *e.LastAcceptedAt)
		if perr == nil && now.Sub(at) < mergeWindow {
			e.SuppressedCount++
			return true, 0, r.writeMerge(ctx, h, m, ms)
		}
		count = e.SuppressedCount
	}
	return false, count, nil
}

// mergeAccepted は少なくとも 1 つの宛先が受理したときだけ last_accepted_at を置き、送らなかった数を 0 にする。24 時間を過ぎて数が 0 の要素は消す。
func (r *Recorder) mergeAccepted(ctx context.Context, typ string, key *string, res []contract.DeliveryResult) error {
	var latest *time.Time
	for _, d := range res {
		if d.Delivered && d.AcceptedAt != nil && (latest == nil || d.AcceptedAt.After(*latest)) {
			t := *d.AcceptedAt
			latest = &t
		}
	}
	if latest == nil {
		return nil
	}
	h, err := r.State.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	m, ms, err := r.readNotify(ctx)
	if err != nil {
		return err
	}
	at := latest.UTC().Format(time.RFC3339)
	found := false
	for i := range ms {
		if ms[i].Type == typ && sameKey(ms[i].Key, key) {
			ms[i].LastAcceptedAt, ms[i].SuppressedCount, found = &at, 0, true
		}
	}
	if !found {
		ms = append(ms, mergeEntry{Type: typ, Key: key, LastAcceptedAt: &at})
	}
	kept := ms[:0]
	for _, e := range ms {
		if p, err := time.Parse(time.RFC3339, derefStr(e.LastAcceptedAt)); err == nil && latest.Sub(p) >= mergeWindow && e.SuppressedCount == 0 {
			continue
		}
		kept = append(kept, e)
	}
	return r.writeMerge(ctx, h, m, kept)
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (r *Recorder) writeMerge(ctx context.Context, h contract.StateLockHandle, m map[string]json.RawMessage, ms []mergeEntry) error {
	if ms == nil {
		ms = []mergeEntry{}
	}
	m["merge"], _ = json.Marshal(ms)
	b, _ := json.Marshal(m)
	return r.State.Update(ctx, h, "notify", b)
}
