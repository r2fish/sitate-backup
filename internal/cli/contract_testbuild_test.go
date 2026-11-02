//go:build sitate_testbuild

package cli

// 試験ビルドの契約: build が test で、test-tick は共有の模擬時間の値が無いとき ARG_INVALID(format)で止まる(作業指示書 5.1 の GB-101)。

import (
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func TestContractCLITestBuild(t *testing.T) {
	e, code := runOne(t, "version")
	if code != 0 || e.Result["build"] != "test" {
		t.Fatalf("exit=%d result.build=%v", code, e.Result["build"])
	}
}

func TestContractCLITestTickNeedsSimTime(t *testing.T) {
	t.Setenv(contract.EnvSimTimeFile, "")
	e, code := runOne(t, "test-tick", contract.TestTickFlagSetStart, contract.TestTickStart+"Z")
	if code != 2 || e.Error == nil || e.Error.Code != string(contract.CodeArgInvalid) || e.Error.Detail["reason"] != "format" {
		t.Fatalf("exit=%d %s", code, e.Raw)
	}
}
