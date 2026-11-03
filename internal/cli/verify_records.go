package cli

// verify の入力(記録した版 ID)と、始めに確かめる連鎖(spec 3.1.2 の verify、4.2、4.6)。

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type recordedObj struct {
	Key         string `json:"key"`
	Ref         string `json:"ref"`
	VersionID   string `json:"version_id"`
	Size        int64  `json:"size"`
	ETag        string `json:"etag"`
	RetainUntil string `json:"retain_until"`
	WrittenAt   string `json:"written_at"`
	StoredAt    string `json:"stored_at"`
}

func parseT(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// recordedRefs は verify の期待値: state.json の objects[](キー)、snapshots[](マニフェスト)、索引のチャンク(spec 4.2「版 ID の固定」)。
func (d *Deps) recordedRefs(ctx context.Context) ([]contract.VersionRef, error) {
	var out []contract.VersionRef
	for _, item := range []string{"objects", "snapshots"} {
		raw, err := d.State.Get(ctx, item)
		if err != nil {
			return nil, err
		}
		var list []recordedObj
		if len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &list) != nil {
			return nil, internalErr()
		}
		for _, o := range list {
			key, written := o.Key, parseT(o.WrittenAt)
			if item == "snapshots" {
				key, written = "manifests/"+o.Ref, parseT(o.StoredAt)
			}
			if key == "" || o.VersionID == "" {
				continue
			}
			out = append(out, contract.VersionRef{Key: key, VersionID: o.VersionID, Size: o.Size, ETag: o.ETag, RetainUntil: parseT(o.RetainUntil), WrittenAt: written})
		}
	}
	chunks, err := d.Index.Records(ctx)
	if err != nil {
		return nil, err
	}
	return append(out, chunks...), nil
}

type journalChainState struct {
	ok   bool
	kind string
	seq  any
}

// journalChain は状態ディレクトリの journal/ の連鎖を 4.2 の (3)〜(5) で確かめる: 連番が飛ばない(gap)、同じ連番が 1 つだけ(fork)、
// prev_hash が 1 つ前の項目の entry_hash と一致する(prev_mismatch)。始点は最も古い項目(範囲の外の項目は延長せずに消えるので、seq 1 でなくても切れとしない)。
// (1)(2) は Entries が復号するときに確かめる(外れれば誤り)。保管先の journal/ の版の照合と head_missing は journal verify の持ち場(イテレーション 2)。
func (d *Deps) journalChain(ctx context.Context) (journalChainState, error) {
	items, err := d.journal(ctx)
	if err != nil {
		return journalChainState{}, err
	}
	for i := 1; i < len(items); i++ {
		switch {
		case items[i].Seq == items[i-1].Seq:
			return journalChainState{kind: "fork", seq: items[i].Seq}, nil
		case items[i].Seq != items[i-1].Seq+1:
			return journalChainState{kind: "gap", seq: items[i].Seq}, nil
		case items[i].PrevHash == nil || *items[i].PrevHash != items[i-1].EntryHash:
			return journalChainState{kind: "prev_mismatch", seq: items[i].Seq}, nil
		}
	}
	return journalChainState{ok: true}, nil
}

// pendingChainWarnings は pending/log.jsonl の連鎖を確かめ、切れていれば警告 PENDING_CHAIN_BROKEN にして他の照合を続ける(spec 3.1.2)。
// 確かめは Approval の読み取り(pending)で行う。
func (d *Deps) pendingChainWarnings(ctx context.Context) []contract.Warning {
	if d.Approval == nil {
		return []contract.Warning{}
	}
	_, err := d.Approval.Pending(ctx, contract.Call{Words: []string{"pending"}, Args: map[string][]string{}})
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodePendingChainBroken {
		return []contract.Warning{}
	}
	return []contract.Warning{{Code: ce.Code, Detail: ce.Detail, Next: []contract.Next{}}}
}

// markRefsVerified は欠け・保持不足・記録外の版が無かった verify の終わりに、世代の参照の存在を確かめた記録を残す(spec 4.2「記録から作る値」):
// journal/ に refs_verified の項目(refs[]、listed_at)を書き、snapshots[] の refs_verified を true にする。どれかの参照に欠けがあったときは何も変えない
// (確かめられた世代を特定できないので、確認済みにしない)。記録の失敗は verify の結果を変えない。
func (d *Deps) markRefsVerified(ctx context.Context, runID string) {
	raw, err := d.State.Get(ctx, "snapshots")
	var snaps []map[string]json.RawMessage
	if err != nil || json.Unmarshal(raw, &snaps) != nil || len(snaps) == 0 {
		return
	}
	var refs []string
	for _, s := range snaps {
		var ref string
		_ = json.Unmarshal(s["ref"], &ref)
		refs = append(refs, ref)
		s["refs_verified"] = json.RawMessage("true")
	}
	var lv struct {
		ListedAt string `json:"listed_at"`
	}
	if lraw, e := d.State.Get(ctx, "last_verify"); e == nil {
		_ = json.Unmarshal(lraw, &lv)
	}
	nb, _ := json.Marshal(snaps)
	h, err := d.State.Lock(ctx)
	if err != nil {
		return
	}
	_ = d.State.Update(ctx, h, "snapshots", nb)
	h.Release()
	_ = d.recordItem(ctx, "refs_verified", runID, map[string]any{"refs": refs, "listed_at": lv.ListedAt})
}
