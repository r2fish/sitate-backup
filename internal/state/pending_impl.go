package state

// このイテレーションの後の節(進捗ファイル参照)で実装する StateFiles の操作。

import (
	"context"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// RecordReadStart は contract.StateFiles を満たす(読み出しの精算はイテレーション 3)。
func (s *StateFiles) RecordReadStart(context.Context, contract.ReadStart) error {
	return contract.ErrNotImplemented("state.StateFiles.RecordReadStart")
}

// SettleInterrupted は contract.StateFiles を満たす(読み出しの精算はイテレーション 3)。
func (s *StateFiles) SettleInterrupted(context.Context, string) error {
	return contract.ErrNotImplemented("state.StateFiles.SettleInterrupted")
}
