package approval

// 種別ごとの引数の検査(propose)。種別と内容の不一致で遅延を飛ばせないようにする(spec 3.6、4.1 の任意項目の表)。

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func checkDelayHours(args, plan map[string]json.RawMessage) error {
	var n int
	_ = json.Unmarshal(args["hours"], &n)
	if n < minDelayHours || n > delayHoursMax {
		return invalidDelay()
	}
	plan["applies_to"] = json.RawMessage(`"later_proposals"`)
	return nil
}

// 復元テストの頻度の順(4.1: monthly / weekly)。
var freqRank = map[string]int{"monthly": 1, "weekly": 2}

func (a *Approval) checkFrequency(ctx context.Context, kind string, args, plan map[string]json.RawMessage) error {
	var to string
	_ = json.Unmarshal(args["to"], &to)
	if freqRank[to] == 0 {
		return argErr("--to", "format")
	}
	norm, _, err := a.State.ReadDef(ctx)
	if err != nil {
		return err
	}
	var d struct {
		RestoreTest string `json:"restore_test"`
	}
	_ = json.Unmarshal(norm, &d)
	from := d.RestoreTest
	if freqRank[from] == 0 {
		from = "monthly"
	}
	up := freqRank[to] > freqRank[from]
	if (kind == "restore-test-increase") != up || freqRank[to] == freqRank[from] {
		return argErr("--to", "mismatch") // 種別と変更の向きが合わない(増加は遅延 0、低下は遅延あり)
	}
	plan["from"], plan["to"] = mustJSON(from), mustJSON(to)
	return nil
}

// checkTo は restore-elsewhere・storage-read-extra・restore-session-extend の --to を必須にし、バックアップサーバーの外の宛先の接頭辞を照らす(gate と同じ規則)。
// restore-session-extend は開いているセッションの延長(--session)なら --to を要さない。
func checkTo(kind string, args map[string]json.RawMessage) error {
	if _, ok := args["session"]; ok && kind == "restore-session-extend" {
		return nil
	}
	var to string
	if raw, ok := args["to"]; !ok || json.Unmarshal(raw, &to) != nil {
		return argErr("--to", "missing")
	}
	l, err := loadGate()
	if err != nil {
		return internalErr()
	}
	if !slices.ContainsFunc(l.RestoreToPrefix, func(p string) bool { return strings.HasPrefix(to, p) }) {
		return apiErr(contract.CodeGateDenied, map[string]any{"reason": gateLocal, "index": nil})
	}
	return nil
}

type notifySection struct {
	To           []string `json:"to"`
	SmtpRef      *string  `json:"smtp_ref"`
	HeartbeatRef *string  `json:"heartbeat_ref"`
}

func eqPtr(a, b *string) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

// checkNotify: notify-add は変更前が変更後の部分集合(削除を含む変更は notify-change)。確認語を入れさせる宛先 proof_required[] は
// 変更前後の notify.to の差(足した宛先。smtp_ref を替えたときは人向けのすべての宛先)から核が計算する。
func (a *Approval) checkNotify(ctx context.Context, kind string, args, plan map[string]json.RawMessage) error {
	var after, before notifySection
	norm, _, err := a.State.ReadDef(ctx)
	if err != nil {
		return err
	}
	canon, err := a.checkSection(ctx, norm, "notify", args["notify"])
	if err != nil {
		return err
	}
	args["notify"] = canon
	if json.Unmarshal(args["notify"], &after) != nil {
		return argErr("--notify", "format")
	}
	var d struct {
		Notify notifySection `json:"notify"`
	}
	_ = json.Unmarshal(norm, &d)
	before = d.Notify
	if kind == "notify-add" {
		subset := true
		for _, t := range before.To {
			subset = subset && slices.Contains(after.To, t)
		}
		keepRef := func(b, af *string) bool { return b == nil || eqPtr(b, af) }
		if !subset || !keepRef(before.SmtpRef, after.SmtpRef) || !keepRef(before.HeartbeatRef, after.HeartbeatRef) {
			return argErr("--notify", "mismatch")
		}
	}
	added := []string{}
	for _, t := range after.To {
		if !slices.Contains(before.To, t) && !slices.Contains(added, t) {
			added = append(added, t)
		}
	}
	if !eqPtr(before.SmtpRef, after.SmtpRef) && after.SmtpRef != nil {
		added = slices.Clone(after.To)
	}
	slices.Sort(added)
	plan["proof_required"], plan["machine"] = mustJSON(added), json.RawMessage(`[]`)
	plan["before"], plan["after"] = mustJSON(before), args["notify"]
	return nil
}
