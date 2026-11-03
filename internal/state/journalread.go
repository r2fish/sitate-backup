package state

// journal/ の読み取り(spec 4.5)。cli が実行の完了済みの判定(3.1.1 の規則 7・8)に使う。契約の Recorder には読み取りの口が無い。

import (
	"context"
	"encoding/json"
	"strconv"
)

// JournalItem は復号した journal/ の項目の平文(spec 4.5 の「journal/ の項目」)。
type JournalItem struct {
	Seq   uint64          `json:"seq"`
	At    string          `json:"at"`
	Kind  string          `json:"kind"`
	RunID string          `json:"run_id"`
	Body  json.RawMessage `json:"body"`
	// PrevHash は平文の prev_hash(1 つ前の項目の entry_hash。seq 1 は null)。EntryHash は項目のキーの entry_hash(連鎖の検証 (5) に使う)。
	PrevHash  *string `json:"prev_hash"`
	EntryHash string  `json:"-"`
}

// Entries は状態ディレクトリの journal/ の項目を連番の順に復号して返す。復号できない項目があれば INTERNAL。
func (r *Recorder) Entries(_ context.Context) ([]JournalItem, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	names, err := r.listLocal()
	if err != nil {
		return nil, err
	}
	key, err := r.KeyS.DerivedKey("journal")
	if err != nil {
		return nil, err
	}
	out := make([]JournalItem, 0, len(names))
	for _, n := range names {
		seq, _ := strconv.ParseUint(journalItemRe.FindStringSubmatch(n)[1], 10, 64)
		b, err := r.readLocal(n)
		if err != nil {
			return nil, err
		}
		plain, err := r.Codec.Decode("SITJ", seqID(seq), b, key)
		if err != nil {
			return nil, internalErr()
		}
		var it JournalItem
		if json.Unmarshal(plain, &it) != nil {
			return nil, internalErr()
		}
		it.EntryHash = journalItemRe.FindStringSubmatch(n)[2]
		out = append(out, it)
	}
	return out, nil
}
