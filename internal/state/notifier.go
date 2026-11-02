package state

// 通知の配送(spec 4.8、3.2.5、7.5 Notifier・TestNotifier)。宛先ごとに通知アダプタを AdapterRunner で呼び、受理しなければ同じ id で送り直す。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/tyler-smith/go-bip39"
	"golang.org/x/text/unicode/norm"
)

// Notifier は contract.Notifier の実物。Secrets があれば、Webhook の宛先(参照名)の URL を secrets から引いて枠 url で渡す。
// Backoff は最初の間隔(ゼロなら contract.NotifyBackoffBase)、Timeout は 1 回の呼び出しの締め切り(ゼロなら contract.NotifyTimeout)、
// RetryMax は送る回数の上限(ゼロなら contract.NotifyRetryMax)。
type Notifier struct {
	State    contract.StateFiles
	Adapters contract.AdapterRunner
	Clock    contract.Clock
	Secrets  contract.Secrets

	Backoff, Timeout time.Duration
	RetryMax         int
}

var _ contract.Notifier = (*Notifier)(nil)

type wireEvent struct {
	Schema          string          `json:"schema"`
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Severity        string          `json:"severity"`
	Time            string          `json:"time"`
	RepoID          *string         `json:"repo_id"`
	Facts           json.RawMessage `json:"facts"`
	SuppressedCount int             `json:"suppressed_count"`
	Next            []contract.Next `json:"next"`
}

func eventJSON(ev contract.Event) json.RawMessage {
	w := wireEvent{Schema: "sitate.event/v1", ID: ev.ID, Type: ev.Type, Severity: ev.Severity, Time: ev.Time.UTC().Format(time.RFC3339), RepoID: ev.RepoID,
		Facts: ev.Facts, SuppressedCount: ev.SuppressedCount, Next: ev.Next}
	if len(w.Facts) == 0 {
		w.Facts = json.RawMessage("{}")
	}
	if w.Next == nil {
		w.Next = []contract.Next{}
	}
	b, _ := json.Marshal(w)
	return b
}

// Deliver は contract.Notifier を満たす。宛先ごとに独立に送り、1 つの失敗は他を止めない。配送の間は state.lock を持たない。
// 配送の失敗は誤りにせず結果の要素(delivered=false と reason)で返す。
func (n *Notifier) Deliver(ctx context.Context, ev contract.Event, to []string) ([]contract.DeliveryResult, error) {
	out := make([]contract.DeliveryResult, 0, len(to))
	for _, addr := range to {
		out = append(out, n.deliverOne(ctx, ev, addr))
	}
	return out, nil
}

// Resend は contract.Notifier を満たす。state.json の notify.pending[] のイベントを同じ id で送り直す(イテレーション 3。送るものが無ければ空の配列)。
func (n *Notifier) Resend(context.Context) ([]contract.DeliveryResult, error) {
	if n.State == nil || n.Adapters == nil || n.Clock == nil {
		return nil, internalErr()
	}
	return []contract.DeliveryResult{}, nil
}

func (n *Notifier) deliverOne(ctx context.Context, ev contract.Event, to string) contract.DeliveryResult {
	res := contract.DeliveryResult{To: to}
	max, backoff, timeout := n.RetryMax, n.Backoff, n.Timeout
	if max <= 0 {
		max = contract.NotifyRetryMax
	}
	if backoff <= 0 {
		backoff = contract.NotifyBackoffBase
	}
	if timeout <= 0 {
		timeout = contract.NotifyTimeout
	}
	req, name, err := n.request(ctx, ev, to)
	if err != nil {
		res.Reason = "adapter_failed"
		return res
	}
	for attempt := 1; attempt <= max; attempt++ {
		res.Attempts = attempt
		ok, channel, reason, retry := n.call(ctx, name, req, timeout)
		if channel != "" {
			res.Channel = channel
		}
		if ok {
			now, _ := n.Clock.Now(ctx)
			res.Delivered, res.Reason, res.AcceptedAt = true, "", &now
			return res
		}
		res.Reason = reason
		if !retry || attempt == max {
			return res
		}
		select { // 間隔は回ごとに 2 倍
		case <-ctx.Done():
			return res
		case <-time.After(backoff << (attempt - 1)):
		}
	}
	return res
}

// request は通知アダプタの要求(spec 3.2.5)を組む。宛先に `@` があれば email、なければ Webhook の参照名(url を secrets から引く)。
func (n *Notifier) request(ctx context.Context, ev contract.Event, to string) (json.RawMessage, string, error) {
	secrets := map[string]string{}
	name := "email"
	if !strings.Contains(to, "@") {
		name = "webhook"
		if n.Secrets == nil {
			return nil, "", errors.New("no secrets")
		}
		raw, err := n.Secrets.Get(ctx, to)
		if err != nil {
			return nil, "", err
		}
		var v struct { // 秘密ファイルの kind: token(spec 4.5 の「参照名と秘密ファイル」)。値が Webhook の URL
			Value string `json:"value"`
		}
		if json.Unmarshal(raw, &v) != nil || v.Value == "" {
			return nil, "", errors.New("no url")
		}
		secrets["url"] = v.Value
	}
	b, _ := json.Marshal(map[string]any{"contract": "sitate.notify/v1", "to": to, "event": json.RawMessage(eventJSON(ev)), "secrets": secrets})
	return b, name, nil
}

// call は通知アダプタを 1 回呼ぶ。retry は同じ id での送り直しに値するか(受理されない結果、時限切れ、アダプタの一時的な失敗)。
func (n *Notifier) call(ctx context.Context, name string, req json.RawMessage, timeout time.Duration) (ok bool, channel, reason string, retry bool) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := n.Adapters.Run(cctx, contract.RunContext{ProcessGroup: true, PdeathSig: true}, "notify", name, req, nil)
	if err != nil {
		var ce *contract.Error
		if errors.As(err, &ce) && ce.Code == contract.CodeAdapterTimeout || errors.Is(err, context.DeadlineExceeded) {
			return false, "", "timeout", true
		}
		return false, "", "adapter_failed", errors.As(err, &ce) && ce.Code == contract.CodeAdapterError
	}
	var r struct {
		Delivered bool   `json:"delivered"`
		Channel   string `json:"channel"`
		Reason    string `json:"reason"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return false, "", "adapter_failed", false
	}
	if r.Delivered {
		return true, r.Channel, "", false
	}
	return false, r.Channel, r.Reason, true
}

// TestNotifier は contract.TestNotifier の実物。確認語は宛先ごとに別の語で、残すのは塩とハッシュだけ(spec 4.8、4.5 の notify.test_proofs[])。
type TestNotifier struct {
	State    contract.StateFiles
	Adapters contract.AdapterRunner
	Clock    contract.Clock
	Secrets  contract.Secrets
	// RunID はイベントの id に使う実行の run_id(空なら時刻から作る)。Notifier は配送の設定(空ならそれぞれの既定)。
	RunID    string
	Notifier *Notifier
}

var _ contract.TestNotifier = (*TestNotifier)(nil)

// Test は contract.TestNotifier を満たす。宛先ごとに別のイベント(notify.test、別の id)を作って送り、宛先ごとの確認語のハッシュを返す。
// 配送の結果は呼び出し側(approval)が使う記録に出さない(確認語は出力・ログに書かない)。
func (t *TestNotifier) Test(ctx context.Context, to []string) (map[string]string, error) {
	h, _, _, err := t.TestWithResults(ctx, to)
	return h, err
}

// TestWithResults は Test に加えて、宛先ごとの配送の結果と確認語を送った時刻を返す(`notify test` の結果。contract.TestNotifier は結果を返さない)。
func (t *TestNotifier) TestWithResults(ctx context.Context, to []string) (map[string]string, []contract.DeliveryResult, time.Time, error) {
	hashes, res, now, err := t.test(ctx, to)
	return hashes, res, now, err
}

func (t *TestNotifier) test(ctx context.Context, to []string) (map[string]string, []contract.DeliveryResult, time.Time, error) {
	if len(to) == 0 {
		return nil, nil, time.Time{}, newErr(contract.CodeArgInvalid, map[string]any{"flag": "--to", "reason": "missing"})
	}
	now, err := t.Clock.Now(ctx)
	if err != nil {
		return nil, nil, now, err
	}
	n := t.Notifier
	if n == nil {
		n = &Notifier{}
	}
	nn := *n
	nn.State, nn.Adapters, nn.Clock, nn.Secrets = t.State, t.Adapters, t.Clock, t.Secrets
	base := strings.TrimPrefix(t.RunID, "r-")
	if base == "" {
		base = now.Format("20060102T150405Z") + "-0000"
	}
	hashes, proofs, results := map[string]string{}, []map[string]string{}, []contract.DeliveryResult{}
	for i, addr := range to {
		word, salt := confirmationWord(), newSalt()
		h := wordHash(salt, word)
		saltB64 := base64.StdEncoding.EncodeToString(salt)
		hashes[addr] = saltB64 + "|" + h // 契約の注釈: <word_salt>|<word_hash>
		proofs = append(proofs, map[string]string{"to": addr, "word_salt": saltB64, "word_hash": h})
		facts, _ := json.Marshal(map[string]any{"confirmation_word": word})
		ev := contract.Event{ID: "e-" + base + "-" + twoDigits(i+1), Type: "notify.test", Severity: "info", Time: now, Facts: facts, Next: []contract.Next{}}
		results = append(results, nn.deliverOne(ctx, ev, addr))
	}
	return hashes, results, now, t.saveProofs(ctx, now, proofs)
}

func (t *TestNotifier) saveProofs(ctx context.Context, now time.Time, proofs []map[string]string) error {
	h, err := t.State.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	cur, err := t.State.Get(ctx, "notify")
	if err != nil {
		return err
	}
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(cur, &m)
	if m == nil { // notify の項目が無い(null)状態から始める
		m = map[string]json.RawMessage{}
	}
	m["last_test_at"], _ = json.Marshal(now.UTC().Format(time.RFC3339))
	m["test_proofs"], _ = json.Marshal(proofs)
	b, _ := json.Marshal(m)
	return t.State.Update(ctx, h, "notify", b)
}

func twoDigits(n int) string { return fmt.Sprintf("%02d", n) }

// confirmationWord は宛先ごとの確認語: BIP-39 の英語の単語表(2,048 語)から無作為に選んだ 3 語を半角空白 1 つで区切る(spec 3.1.2.10)。
func confirmationWord() string {
	words := bip39.GetWordList()
	pick := make([]string, 3)
	for i := range pick {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(words))))
		pick[i] = words[n.Int64()]
	}
	return strings.Join(pick, " ")
}

// newSalt は 16 バイトの無作為の塩(バイト列)。
func newSalt() []byte {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return b
}

// wordHash は塩のバイト列と正規化した確認語(NFKC、小文字、前後の空白なし、続く空白は 1 つ)の UTF-8 をこの順につないだ値の SHA-256(spec 3.1.2.10)。
func wordHash(salt []byte, word string) string {
	w := strings.Join(strings.Fields(strings.ToLower(norm.NFKC.String(word))), " ")
	sum := sha256.Sum256(append(append([]byte{}, salt...), w...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
