//go:build !sitate_testbuild

package cli

// 製品ビルドの契約: build が product で、試験ビルドだけの test-tick を受け付けない(3.3、spec 6.5 A11 の一部)。

import (
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func TestContractCLIProductBuild(t *testing.T) {
	e, code := runOne(t, "version")
	if code != 0 || e.Result["build"] != "product" {
		t.Fatalf("exit=%d result.build=%v", code, e.Result["build"])
	}
}

func TestContractCLITestTickRejectedInProductBuild(t *testing.T) {
	e, code := runOne(t, "test-tick", contract.TestTickFlagDays, "1")
	if code != 2 || e.Error == nil || e.Error.Code != string(contract.CodeArgInvalid) || e.Error.Detail["reason"] != "unknown" {
		t.Fatalf("exit=%d %s", code, e.Raw)
	}
}
