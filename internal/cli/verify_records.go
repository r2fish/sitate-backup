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

// journalChain は状態ディレクトリの journal/ の連番の連続を確かめる(飛びは gap、重なりは fork)。
// ハッシュ連鎖そのもの(prev_mismatch)と保管先の journal/ の先頭の欠けは journal verify の持ち場で、ここでは見ない。
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
