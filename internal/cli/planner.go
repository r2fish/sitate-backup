package cli

// approval に渡す cli 側の継ぎ目(spec 7.5: propose の plan の組み立てと標準入力の読み取りは cli)。結線(完了条件 24 と 5.18)が
// approval.Approval の Planner と ReadDefinition にこの 2 つを入れる。

import (
	"context"
	"encoding/json"
	"io"
	"os"
)

// ReadDefinition は propose seal・def-change の --file の内容を読む。"-" は標準入力(--secrets-stdin の 1 行目の後に続けて読む)。
func (d *Deps) ReadDefinition(_ context.Context, file string) ([]byte, error) {
	var b []byte
	var err error
	if file == "-" {
		r := d.stdinReader()
		if r == nil {
			return nil, argErr("--file", "missing")
		}
		b, err = io.ReadAll(r)
	} else {
		b, err = os.ReadFile(file)
	}
	if err != nil {
		return nil, argErr("--file", "format")
	}
	return b, nil
}

// Planner は plan の追加項目と前提の材料(approval.PlanHook と同じ形)。この版が組むのは hold-release の plan.causes[]・since・
// days_since_last_ok と、前提の材料 hold(since・causes[])。費用・読み出し量・ホスト鍵の取得を伴う種別の plan はそれぞれの部品の結線で足す。
func (d *Deps) Planner(ctx context.Context, op string, _ map[string]json.RawMessage) (plan, material map[string]json.RawMessage, err error) {
	if op != "hold-release" || d.State == nil {
		return nil, nil, nil
	}
	raw, err := d.State.Get(ctx, "hold")
	if err != nil {
		return nil, nil, err
	}
	var h struct {
		Since  json.RawMessage `json:"since"`
		Causes json.RawMessage `json:"causes"`
	}
	_ = json.Unmarshal(raw, &h)
	causes, since := orEmptyList(h.Causes), orNull(h.Since)
	days := json.RawMessage("null")
	if base, ok := d.autoHoldBase(ctx); ok && d.Clock != nil {
		if now, e := d.Clock.Now(ctx); e == nil {
			days = mustMarshal(int(now.Sub(base).Hours() / 24))
		}
	}
	plan = map[string]json.RawMessage{"causes": causes, "since": since, "days_since_last_ok": days}
	material = map[string]json.RawMessage{"hold": mustMarshal(map[string]json.RawMessage{"since": since, "causes": causes})}
	return plan, material, nil
}
