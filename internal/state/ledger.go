package state

// 復元先の資源の台帳 ledger/<record_id>.json(spec 4.5 の「ledger/ の記録」)。作る・更新する・消すのは state.lock の下。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

var recordIDRe = regexp.MustCompile(`^l-[0-9a-f]{16}$`)

// LedgerAppend は contract.StateFiles を満たす。record_id が無ければ `l-` と乱数 64 ビットの 16 進 16 文字を付ける。同じ record_id は置き換える(更新)。
func (s *StateFiles) LedgerAppend(_ context.Context, held contract.StateLockHandle, rec json.RawMessage) error {
	if err := s.heldBy(held); err != nil {
		return err
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(rec, &m); err != nil || m == nil {
		return newErr(contract.CodeArgInvalid, map[string]any{"flag": "record", "reason": "format"})
	}
	var id string
	if raw, ok := m["record_id"]; ok {
		if json.Unmarshal(raw, &id) != nil || !recordIDRe.MatchString(id) {
			return newErr(contract.CodeArgInvalid, map[string]any{"flag": "record", "reason": "format"})
		}
	} else {
		var r [8]byte
		_, _ = rand.Read(r[:])
		id = "l-" + hex.EncodeToString(r[:])
		m["record_id"], _ = json.Marshal(id)
	}
	b, _ := json.Marshal(m)
	if err := s.writeAtomic(s.path("ledger", id+".json"), append(b, '\n')); err != nil {
		return internalErr()
	}
	return nil
}

// LedgerList は contract.StateFiles を満たす。record_id の順。途中で止まって残った一時ファイル(.tmp-)は読まない。ロックは要さない(読むだけ)。
func (s *StateFiles) LedgerList(context.Context) ([]json.RawMessage, error) {
	ents, err := os.ReadDir(s.path("ledger"))
	if errors.Is(err, os.ErrNotExist) {
		return []json.RawMessage{}, nil
	}
	if err != nil {
		return nil, internalErr()
	}
	names := []string{}
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && !strings.HasPrefix(e.Name(), ".tmp-") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([]json.RawMessage, 0, len(names))
	for _, n := range names {
		b, err := os.ReadFile(s.path("ledger", n))
		if err != nil || !json.Valid(b) {
			return nil, internalErr()
		}
		out = append(out, json.RawMessage(strings.TrimRight(string(b), "\n")))
	}
	return out, nil
}

// LedgerDelete は contract.StateFiles を満たす。無い記録の削除は成功(冪等)。unlink してディレクトリを fsync する。
func (s *StateFiles) LedgerDelete(_ context.Context, held contract.StateLockHandle, recordID string) error {
	if err := s.heldBy(held); err != nil {
		return err
	}
	if !recordIDRe.MatchString(recordID) {
		return newErr(contract.CodeArgInvalid, map[string]any{"flag": "record", "reason": "format"})
	}
	if err := os.Remove(s.path("ledger", recordID+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return internalErr()
	}
	if err := syncDir(s.path("ledger")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return internalErr()
	}
	return nil
}
