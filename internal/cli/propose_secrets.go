package cli

// propose notify-add・notify-change の資格情報(spec 3.1.2.9: 足す・差し替える資格情報は使っていない新しい参照名で JSON に書き、
// 値を --secrets-stdin で差し込む。核は作成時にその値を秘密ファイルへ置いて試験通知に使い、提案が作れなければ消す。使用中の参照名への上書きは ARG_INVALID)。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func proposeEntry(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
	if d.Approval == nil {
		return notImpl("propose")
	}
	if len(c.Positional) != 1 || (c.Positional[0] != "notify-add" && c.Positional[0] != "notify-change") || len(c.Secrets) == 0 || c.DryRun || d.Secrets == nil {
		return d.Approval.Propose(ctx, c)
	}
	var placed []string
	for ref, raw := range c.Secrets {
		if !secretRefRe.MatchString(ref) {
			return contract.Reply{}, argErr("--secrets-stdin", "format")
		}
		if _, err := d.Secrets.Get(ctx, ref); err == nil { // 使用中の参照名は上書きしない
			dropPlaced(ctx, d, placed)
			return contract.Reply{}, argErr("--secrets-stdin", "mismatch")
		}
		var val string
		if json.Unmarshal(raw, &val) != nil || val == "" {
			dropPlaced(ctx, d, placed)
			return contract.Reply{}, argErr("--secrets-stdin", "format")
		}
		file, _ := json.Marshal(map[string]string{"kind": "token", "value": val})
		if err := d.Secrets.Put(ctx, ref, file); err != nil {
			dropPlaced(ctx, d, placed)
			return contract.Reply{}, err
		}
		placed = append(placed, ref)
	}
	rep, err := d.Approval.Propose(ctx, c)
	if err != nil {
		dropPlaced(ctx, d, placed) // 提案を作れなかった。置いた値を残さない
	}
	return rep, err
}

func dropPlaced(ctx context.Context, d *Deps, refs []string) {
	for _, r := range refs {
		_ = d.Secrets.Delete(ctx, r)
	}
}
