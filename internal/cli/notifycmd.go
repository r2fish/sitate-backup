package cli

// notify set・notify test(spec 3.1.2、3.1.2.2)。init の前でも動く。

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func init() {
	routeOwn["notify set"] = notifySet
	routeOwn["notify test"] = notifyTest
}

// testNotifyResults は contract.TestNotifier が返さない配送の結果を出す実物(state.TestNotifier)の口。
type testNotifyResults interface {
	TestWithResults(ctx context.Context, to []string) (map[string]string, []contract.DeliveryResult, time.Time, error)
}

// notifyChannel は宛先からチャネルを決める(`@` があればメール、なければ Webhook の参照名。spec 4.1)。
func notifyChannel(to string) string {
	if strings.Contains(to, "@") {
		return "email"
	}
	return "webhook"
}

// notifySet は資格情報の値を --secrets-stdin のキー <secret-ref> で受け、秘密ファイル(kind: token)に置く。
// Webhook の宛先は参照名そのもので、通知の配送(state)は宛先の名前で秘密ファイルを引くため、--secret-ref は宛先と同じ名前に限る。
func notifySet(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
	if d.Secrets == nil {
		return notImpl("notify-set")
	}
	to, ref := c.Args["to"][0], c.Args["secret-ref"][0]
	if err := notifySetNeedsProposal(ctx, d, to); err != nil {
		return contract.Reply{}, err
	}
	if !secretRefRe.MatchString(ref) || strings.HasPrefix(ref, "sitate-") {
		return contract.Reply{}, argErr("--secret-ref", "format")
	}
	ch := notifyChannel(to)
	if ch == "webhook" && to != ref {
		return contract.Reply{}, argErr("--secret-ref", "format")
	}
	if c.DryRun { // --dry-run は実行しない(spec 3.1.1)。秘密ファイルを置かない
		return notImpl("notify-set.dry-run")
	}
	raw, ok := c.Secrets[ref]
	if !ok {
		return contract.Reply{}, argErr("--secrets-stdin", "missing")
	}
	var val string
	if json.Unmarshal(raw, &val) != nil || val == "" {
		return contract.Reply{}, argErr("--secrets-stdin", "format")
	}
	file, _ := json.Marshal(map[string]string{"kind": "token", "value": val})
	if err := d.Secrets.Put(ctx, ref, file); err != nil {
		return contract.Reply{}, err
	}
	b, _ := json.Marshal(map[string]any{"to": to, "secret_ref": ref, "channel": ch})
	return contract.Reply{Result: b}, nil
}

// notifySetNeedsProposal は seal の後の notify set を拒否する(spec 3.1.2 の notify set の行)。宛先の追加は notify-add、
// 削除・差し替えと資格情報の差し替えは notify-change。承認済みの定義の notify.to に無い宛先は追加、あるか読めなければ差し替えとみなす。
// seal の前と init --adopt の後(sealed が偽)は従来どおり承認なし。
func notifySetNeedsProposal(ctx context.Context, d *Deps, to string) error {
	var sealed bool
	if raw, err := d.State.Get(ctx, "sealed"); err == nil {
		_ = json.Unmarshal(raw, &sealed)
	}
	if !sealed {
		return nil
	}
	kind := "notify-change"
	if norm, _, err := d.State.ReadDef(ctx); err == nil {
		var def struct {
			Notify struct {
				To []string `json:"to"`
			} `json:"notify"`
		}
		if json.Unmarshal(norm, &def) == nil && !slices.Contains(def.Notify.To, to) {
			kind = "notify-add"
		}
	}
	e := newErr(contract.CodeApprovalRequired, map[string]any{"kind": kind})
	e.Next = []contract.Next{{Argv: []string{"sitate", "propose", kind}, RequiresApproval: true}}
	return e
}

// notifyRecipients は定義(承認済み、無ければ候補)の notify.to。
func notifyRecipients(ctx context.Context, d *Deps) ([]string, error) {
	norm, _, err := d.State.ReadDef(ctx)
	if err != nil {
		if e, ok := err.(*contract.Error); !ok || (e.Code != contract.CodeNotSealed && e.Code != contract.CodeNotInitialized) {
			return nil, err
		}
		var found bool
		if norm, _, found, err = d.State.ReadCandidate(ctx); err != nil {
			return nil, err
		}
		if !found {
			return nil, newErr(contract.CodeDefInvalid, map[string]any{"issues": []any{}})
		}
	}
	var def struct {
		Notify struct {
			To []string `json:"to"`
		} `json:"notify"`
	}
	_ = json.Unmarshal(norm, &def)
	return def.Notify.To, nil
}

func notifyTest(d *Deps, ctx context.Context, _ contract.Call) (contract.Reply, error) {
	tn, ok := d.TestNotifier.(testNotifyResults)
	if d.State == nil || !ok {
		return notImpl("notify-test")
	}
	to, err := notifyRecipients(ctx, d)
	if err != nil {
		return contract.Reply{}, err
	}
	_, res, at, err := tn.TestWithResults(ctx, to)
	if err != nil {
		return contract.Reply{}, err
	}
	all, out := true, make([]map[string]any, 0, len(res))
	for _, r := range res {
		var reason, accepted any
		if r.Reason != "" {
			reason = r.Reason
		}
		if r.AcceptedAt != nil {
			accepted = r.AcceptedAt.UTC().Format(time.RFC3339)
		}
		ch := r.Channel
		if ch == "" {
			ch = notifyChannel(r.To)
		}
		out = append(out, map[string]any{"to": r.To, "channel": ch, "delivered": r.Delivered, "attempts": r.Attempts, "reason": reason, "accepted_at": accepted})
		all = all && r.Delivered
	}
	b, _ := json.Marshal(map[string]any{"delivered": out, "all_delivered": all, "proof_issued_at": at.UTC().Format(time.RFC3339)})
	return contract.Reply{Result: b}, nil
}
