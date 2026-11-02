package cli

// apply の run 要素(spec 3.1.2.11「適用の操作の列」の run): 状態だけを書き換える種別(hold-release、key-rotate-waive、approval-key-change)は
// cli が StateFiles を呼んで行い、ほかは entry が指す入口(サブコマンドの名前)を呼ぶ。部品が結線されていない入口は NOT_IMPLEMENTED。

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func (d *Deps) runStep(ctx context.Context, c contract.Call, ch contract.ChangeOps, st contract.OpStep) (json.RawMessage, error) {
	switch st.Entry {
	case "hold-release":
		return d.holdRelease(ctx, ch.ChangeID, c.RunID)
	case "key-rotate-waive":
		return d.stateUpdate(ctx, "key_rotate_required", json.RawMessage("false"))
	case "approval-key-change":
		return d.approvalKeyChange(ctx, st.Args)
	}
	call := stepCall(c, st)
	rep, err := safeEntry(route(call.Words), d, ctx, call)
	if err != nil {
		return nil, err
	}
	if len(rep.Result) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return rep.Result, nil
}

// stepCall は操作の要素の入口を呼ぶ Call。args のキーは提案の正規化(先頭の -- を除き - を _ にした名前)を引数の名前へ戻す。
// 秘密の参照名と秘密の入力は apply の呼び出しのものを引き継ぐ。
func stepCall(c contract.Call, st contract.OpStep) contract.Call {
	args := map[string][]string{}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(st.Args, &m)
	for k, v := range m {
		args[strings.ReplaceAll(k, "_", "-")] = argValues(v)
	}
	for _, k := range []string{"admin-ref", "host-ref", "source-admin-ref"} {
		if v := c.Args[k]; len(v) > 0 {
			args[k] = v
		}
	}
	return contract.Call{Words: strings.Fields(st.Entry), Args: args, RunID: c.RunID, Secrets: c.Secrets,
		SecretOut: c.SecretOut, PhraseIn: c.PhraseIn, SnapshotKeyIn: c.SnapshotKeyIn}
}

func argValues(v json.RawMessage) []string {
	var arr []json.RawMessage
	if json.Unmarshal(v, &arr) == nil {
		out := []string{}
		for _, e := range arr {
			out = append(out, argValues(e)...)
		}
		return out
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		return []string{s}
	}
	var f float64
	if json.Unmarshal(v, &f) == nil {
		return []string{strconv.FormatFloat(f, 'f', -1, 64)}
	}
	var b bool
	if json.Unmarshal(v, &b) == nil {
		return []string{strconv.FormatBool(b)}
	}
	return []string{string(v)}
}

// stateUpdate は state.lock の下で state.json の項目を置き換える。
func (d *Deps) stateUpdate(ctx context.Context, item string, v json.RawMessage) (json.RawMessage, error) {
	if d.State == nil {
		return nil, contract.ErrNotImplemented("cli.apply")
	}
	h, err := d.State.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer h.Release()
	if err := d.State.Update(ctx, h, item, v); err != nil {
		return nil, err
	}
	return json.RawMessage(`{}`), nil
}

// approvalKeyChange は承認鍵の登録(以後の署名は新しい鍵で検証する。spec 3.1.2.9 の approval-key-change)。ed25519 の software 鍵だけ。
func (d *Deps) approvalKeyChange(ctx context.Context, rawArgs json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Pubkey  string `json:"pubkey"`
		KeyType string `json:"key_type"`
	}
	_ = json.Unmarshal(rawArgs, &a)
	if a.KeyType == "" {
		a.KeyType = "software"
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte("ssh-ed25519 " + a.Pubkey))
	if err != nil || a.KeyType != "software" {
		return nil, argErr("--pubkey", "format")
	}
	v, _ := json.Marshal(map[string]string{"key_fp": ssh.FingerprintSHA256(pub), "key_type": a.KeyType, "public_key": "ssh-ed25519 " + a.Pubkey})
	return d.stateUpdate(ctx, "approval_key", v)
}

type holdCauseKey struct {
	Reason string  `json:"reason"`
	Code   *string `json:"code"`
}

// holdRelease は削除停止の解除(spec 4.2。D-0118): state.lock の下で現在の causes[] と提案の plan に載せた causes[] を比べ、
// 載せた要素(reason と code の一致)だけを suppressed[] に移す。載っていない要素(承認の後に自動停止が足した原因)があれば残して停止を続ける
// (hold.set は送らない)。残りが無ければ停止を解く。plan は approval の pending の変更の記録から読む。
func (d *Deps) holdRelease(ctx context.Context, changeID, runID string) (json.RawMessage, error) {
	if d.State == nil || d.Approval == nil {
		return nil, contract.ErrNotImplemented("cli.apply")
	}
	planned, err := d.plannedCauses(ctx, changeID)
	if err != nil {
		return nil, err
	}
	h, err := d.State.Lock(ctx)
	if err != nil {
		return nil, err
	}
	released := false
	release := func() {
		if !released {
			released = true
			h.Release()
		}
	}
	defer release()
	raw, err := d.State.Get(ctx, "hold")
	if err != nil {
		return nil, err
	}
	var hold map[string]json.RawMessage
	if json.Unmarshal(raw, &hold) != nil || hold == nil {
		hold = map[string]json.RawMessage{}
	}
	var causes []map[string]json.RawMessage
	_ = json.Unmarshal(hold["causes"], &causes)
	var suppressed []json.RawMessage
	_ = json.Unmarshal(hold["suppressed"], &suppressed)
	remain := []map[string]json.RawMessage{}
	for _, c := range causes {
		if !containsCause(planned, c) {
			remain = append(remain, c)
			continue
		}
		suppressed = append(suppressed, mustMarshal(map[string]json.RawMessage{"reason": c["reason"], "code": orNull(c["code"]), "refs": orEmptyList(c["refs"])}))
	}
	hold["causes"], hold["suppressed"] = mustMarshal(remain), mustMarshal(nonNilRaw(suppressed))
	if len(remain) == 0 {
		hold["active"], hold["since"], hold["reason"], hold["auto"] = json.RawMessage("false"), json.RawMessage("null"), json.RawMessage("null"), json.RawMessage("false")
	}
	if err := d.State.Update(ctx, h, "hold", mustMarshal(hold)); err != nil {
		return nil, err
	}
	release() // Recorder は state.lock を取るので、持ったまま記録しない
	d.recordItem(ctx, "hold", runID, map[string]any{"active": len(remain) > 0, "reason": nilIf(hold["reason"]), "code": nil, "auto": string(hold["auto"]) == "true"})
	return json.RawMessage(`{}`), nil
}

func nonNilRaw(l []json.RawMessage) []json.RawMessage {
	if l == nil {
		return []json.RawMessage{}
	}
	return l
}

func orNull(v json.RawMessage) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage("null")
	}
	return v
}

func orEmptyList(v json.RawMessage) json.RawMessage {
	if len(v) == 0 || string(v) == "null" {
		return json.RawMessage("[]")
	}
	return v
}

func nilIf(v json.RawMessage) any {
	if len(v) == 0 || string(v) == "null" {
		return nil
	}
	var s any
	_ = json.Unmarshal(v, &s)
	return s
}

func containsCause(planned []holdCauseKey, c map[string]json.RawMessage) bool {
	var k holdCauseKey
	_ = json.Unmarshal(c["reason"], &k.Reason)
	_ = json.Unmarshal(c["code"], &k.Code)
	for _, p := range planned {
		if p.Reason == k.Reason && ((p.Code == nil && k.Code == nil) || (p.Code != nil && k.Code != nil && *p.Code == *k.Code)) {
			return true
		}
	}
	return false
}

// plannedCauses は提案の plan.causes[](承認カードに示した停止の原因)。
func (d *Deps) plannedCauses(ctx context.Context, changeID string) ([]holdCauseKey, error) {
	rep, err := d.Approval.Pending(ctx, contract.Call{Words: []string{"pending"}, Args: map[string][]string{"id": {changeID}}})
	if err != nil {
		return nil, err
	}
	var r struct {
		Changes []struct {
			Plan struct {
				Causes []holdCauseKey `json:"causes"`
			} `json:"plan"`
		} `json:"changes"`
	}
	if json.Unmarshal(rep.Result, &r) != nil || len(r.Changes) != 1 {
		return nil, internalErr()
	}
	return r.Changes[0].Plan.Causes, nil
}
