package main

import (
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// 契約試験(作業指示書 3.2)。WP は変えない。
func TestContractAdapter(t *testing.T) { harness.RunAdapter(t, ".") }
