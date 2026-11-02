package approval

// Proposer: 他の部品(keys)が提案を作る入口。同じ内容の保留中の提案があればその id を返す。

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Proposer は contract.Proposer の実装。
type Proposer struct{ A *Approval }

var _ contract.Proposer = (*Proposer)(nil)

// Propose は content(引数の名前 → 値の JSON オブジェクト)で kind を提案する。
func (p *Proposer) Propose(ctx context.Context, kind string, content json.RawMessage) (string, error) {
	if p == nil || p.A == nil {
		return "", internalErr()
	}
	if err := p.A.ready(); err != nil {
		return "", err
	}
	in := map[string][]string{}
	if len(content) > 0 && string(content) != "null" {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(content, &m); err != nil {
			return "", argErr("--content", "format")
		}
		for k, v := range m {
			in[k] = []string{textOf(v)}
		}
	}
	out, err := p.A.propose(ctx, kind, in, false)
	return out.ChangeID, err
}

func textOf(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(v, &n) == nil {
		if _, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
			return n.String()
		}
	}
	return string(v)
}
