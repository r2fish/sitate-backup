package keys

// doctor の段階と OS の検査(作業指示書 5.7): statfs の結果を OSProbe で差し替えて試す。

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// 状態ディレクトリのファイルシステムが NFS・CIFS・FUSE のとき、doctor は bootstrap 段階で LOCK_UNAVAILABLE(終了 3)で止める。
func TestContractKeysDoctorStateFS(t *testing.T) {
	for _, fs := range contract.FSTypesStateForbidden {
		e := newEnv()
		e.k.OS = &fake.OSProbe{FS: fs}
		_, err := e.k.Doctor(ctx, contract.Call{Words: []string{"doctor"}})
		ce := harness.ExpectCode(t, err, contract.CodeLockUnavailable)
		if ce != nil && contract.CodeTable[ce.Code].Exit != 3 {
			t.Errorf("%s: 終了 3 でない", fs)
		}
	}
	for _, fs := range []string{"ext4", "xfs", "btrfs"} {
		e := newEnv()
		e.k.OS = &fake.OSProbe{FS: fs}
		_, passed, cs := doctor(t, e)
		if c := byName(cs, "state_dir_fs"); c == nil || !c.OK || !passed {
			t.Errorf("%s: state_dir_fs は ok のはず: %+v passed=%v", fs, c, passed)
		}
	}
}

// bootstrap の他の項目は OS の実測の ok をそのまま映し、外れた項目は next[] を持ち passed を偽にする。advisory が外れても passed は変わらない。
func TestContractKeysDoctorOSChecks(t *testing.T) {
	e := newEnv()
	e.k.OS = &fake.OSProbe{Fail: map[string]bool{"container_runtime": true, "swap_disabled": true}}
	_, passed, cs := doctor(t, e)
	if c := byName(cs, "container_runtime"); c == nil || c.OK || len(c.Next) == 0 {
		t.Errorf("container_runtime: %+v", c)
	}
	if c := byName(cs, "swap_disabled"); c == nil || c.OK || c.Severity != "advisory" {
		t.Errorf("swap_disabled: %+v", c)
	}
	if passed {
		t.Error("required が外れているのに passed")
	}
	e = newEnv()
	e.k.OS = &fake.OSProbe{Fail: map[string]bool{"swap_disabled": true}}
	if _, passed, _ := doctor(t, e); !passed {
		t.Error("advisory だけの失敗で passed が偽")
	}
}

// sealed 段階: seal 済みなら timers_registered と file_modes が加わる(表の順で storage_reachable・time_skew の後、advisory の前)。
func TestContractKeysDoctorSealed(t *testing.T) {
	e := newEnv()
	e.st.cand = true
	e.st.StateFiles.Items = map[string]json.RawMessage{"sealed": json.RawMessage("true")}
	e.k.OS = &fake.OSProbe{Fail: map[string]bool{"file_modes": true}}
	stage, passed, cs := doctor(t, e)
	if stage != "sealed" {
		t.Errorf("stage = %q (seal 済みは sealed)", stage)
	}
	want := append(slices.Clone(bootstrapChecks), "storage_reachable", "time_skew", "timers_registered", "file_modes", "adapter_dir_modes", "disk_encryption", "swap_disabled")
	if !slices.Equal(names(cs), want) {
		t.Errorf("checks[] = %v", names(cs))
	}
	if c := byName(cs, "file_modes"); c == nil || c.OK || len(c.Next) == 0 || passed {
		t.Errorf("file_modes は外れ、next[] を持ち、passed は偽: %+v passed=%v", c, passed)
	}
	if c := byName(cs, "timers_registered"); c == nil || !c.OK {
		t.Errorf("timers_registered: %+v", c)
	}
}
