package state

// state.json の項目の読み書き(spec 4.5 の「state.json の項目」)。ファイルは平文の JSON オブジェクトで、書くのは state.lock の下だけ。

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const stateFile = "state.json"

// readState は state.json を項目の表に読む。無ければ空の表(記録を持たない新しいホスト。spec 4.5)。
func (s *StateFiles) readState() (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(s.path(stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, internalErr()
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return nil, internalErr()
	}
	return m, nil
}

// Get は contract.StateFiles を満たす。項目が無ければ JSON の null を返す(未設定。誤りにしない)。
func (s *StateFiles) Get(_ context.Context, item string) (json.RawMessage, error) {
	if item == "" {
		return nil, newErr(contract.CodeArgInvalid, map[string]any{"flag": "item", "reason": "format"})
	}
	m, err := s.readState()
	if err != nil {
		return nil, err
	}
	if v, ok := m[item]; ok {
		return v, nil
	}
	return json.RawMessage("null"), nil
}

// Update は contract.StateFiles を満たす。state.lock を持つ呼び出し側だけが項目を置き換えられる(held が nil か他の StateFiles のものなら誤り)。
func (s *StateFiles) Update(_ context.Context, held contract.StateLockHandle, item string, v json.RawMessage) error {
	if err := s.heldBy(held); err != nil {
		return err
	}
	if item == "" || !json.Valid(v) {
		return newErr(contract.CodeArgInvalid, map[string]any{"flag": "item", "reason": "format"})
	}
	m, err := s.readState()
	if err != nil {
		return err
	}
	m[item] = v
	return s.writeState(m)
}

func (s *StateFiles) writeState(m map[string]json.RawMessage) error {
	b, err := json.Marshal(m)
	if err != nil {
		return internalErr()
	}
	if err := s.writeAtomic(s.path(stateFile), append(b, '\n')); err != nil {
		return internalErr()
	}
	return nil
}
