package main

import (
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// チェックはイテレーション 2(作業指示書 5.10、7 章)。契約試験は「後のイテレーション」の印で skip する。
func TestContractAdapter(t *testing.T) { harness.Later(t, 2, "check") }
