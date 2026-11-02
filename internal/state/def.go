package state

// 定義の置き場 def/approved.json と def/candidate.json(spec 4.5 の「def/ の形」: {"def_hash","normalized"})。

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type defFile struct {
	DefHash    string          `json:"def_hash"`
	Normalized json.RawMessage `json:"normalized"`
}

func (s *StateFiles) readDefFile(name string) (defFile, bool, error) {
	var d defFile
	b, err := os.ReadFile(s.path("def", name))
	if errors.Is(err, os.ErrNotExist) {
		return d, false, nil
	}
	if err != nil || json.Unmarshal(b, &d) != nil || len(d.Normalized) == 0 {
		return d, false, internalErr()
	}
	return d, true, nil
}

// ReadDef は contract.StateFiles を満たす。承認済みの定義の正規化した写しと def_hash。
// 無ければ、記録を持たない新しいホストは NOT_INITIALIZED、`seal` の前は NOT_SEALED。
func (s *StateFiles) ReadDef(ctx context.Context) ([]byte, string, error) {
	d, ok, err := s.readDefFile("approved.json")
	if err != nil {
		return nil, "", err
	}
	if ok {
		return d.Normalized, d.DefHash, nil
	}
	if v, err := s.Get(ctx, "initialized"); err != nil {
		return nil, "", err
	} else if string(v) != "true" {
		return nil, "", newErr(contract.CodeNotInitialized, nil)
	}
	return nil, "", newErr(contract.CodeNotSealed, nil)
}

// ReadCandidate は contract.StateFiles を満たす。`seal` の前の候補(def validate に通った最新の内容)。無ければ found が偽。
func (s *StateFiles) ReadCandidate(context.Context) ([]byte, string, bool, error) {
	d, ok, err := s.readDefFile("candidate.json")
	if err != nil || !ok {
		return nil, "", false, err
	}
	return d.Normalized, d.DefHash, true, nil
}

// StageDef は contract.StateFiles を満たす。候補を置き換える(def_hash は正規化した内容から核が計算する)。
// seal の後は置かない(ALREADY_SEALED)。承認済みの定義の切り替えは SwitchDef だけ。
func (s *StateFiles) StageDef(ctx context.Context, normalized []byte) error {
	if !json.Valid(normalized) {
		return newErr(contract.CodeArgInvalid, map[string]any{"flag": "--file", "reason": "format"})
	}
	if _, ok, err := s.readDefFile("approved.json"); err != nil {
		return err
	} else if ok {
		return newErr(contract.CodeAlreadySealed, nil)
	}
	h, err := contract.CanonicalHash(normalized)
	if err != nil {
		return newErr(contract.CodeArgInvalid, map[string]any{"flag": "--file", "reason": "format"})
	}
	b, _ := json.Marshal(defFile{DefHash: h, Normalized: normalized})
	if err := s.writeAtomic(s.path("def", "candidate.json"), append(b, '\n')); err != nil {
		return internalErr()
	}
	return nil
}
