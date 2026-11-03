//go:build linux

package keys

import (
	"encoding/json"
	"syscall"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const (
	prSetDumpable = 4 // PR_SET_DUMPABLE
	prGetDumpable = 3 // PR_GET_DUMPABLE
	mclCurrent    = 1
	mclFuture     = 2
	mclOnFault    = 4 // 触れたページだけを固定する(Go の予約領域を全部は常駐させない)
	rlimInfinity  = ^uint64(0)
)

func prctl(opt, arg uintptr) (uintptr, syscall.Errno) {
	r, _, e := syscall.Syscall6(syscall.SYS_PRCTL, opt, arg, 0, 0, 0, 0)
	return r, e
}

// Dumpable は PR_GET_DUMPABLE の値(試験用)。
func Dumpable() int { r, _ := prctl(prGetDumpable, 0); return int(r) }

// HardenMemory は起動の始めに 1 回呼ぶ(spec 3.1.1 の「鍵のメモリ」。gate・直接・タイマーのどの起動でも)。
// prctl(PR_SET_DUMPABLE, 0) でコアダンプと ptrace による読み出しを無効にし、プロセスのメモリを mlockall(2) で固定する。
// 鍵は Go の GC 管理のヒープ・スタック・文字列に複数の写しとして散るので、個々のスライスでなくプロセス全体を固定する。
// RLIMIT_MEMLOCK が無制限のときだけ MCL_FUTURE を足す(有限の上限で後からの mmap が ENOMEM になると Go の実行時が落ちるため)。
// 固定に失敗したら警告 MLOCK_FAILED を返し、処理は続ける。子プロセスのメモリ・カーネルのパイプの緩衝は固定できない。
func HardenMemory() []contract.Warning {
	_, _ = prctl(prSetDumpable, 0)
	flags := uintptr(mclCurrent)
	var rl syscall.Rlimit
	if syscall.Getrlimit(8 /* RLIMIT_MEMLOCK */, &rl) == nil && rl.Cur == rlimInfinity {
		flags |= mclFuture
	}
	_, _, e := syscall.Syscall(syscall.SYS_MLOCKALL, flags|mclOnFault, 0, 0)
	if e == syscall.EINVAL { // MCL_ONFAULT を知らない古いカーネル
		_, _, e = syscall.Syscall(syscall.SYS_MLOCKALL, flags, 0, 0)
	}
	if e != 0 {
		return []contract.Warning{mlockWarning(e)}
	}
	return nil
}

func mlockWarning(e syscall.Errno) contract.Warning {
	reason := "enomem"
	switch e {
	case syscall.EPERM:
		reason = "eperm"
	case syscall.EAGAIN:
		reason = "eagain"
	}
	d, _ := json.Marshal(map[string]string{"reason": reason})
	return contract.Warning{Code: contract.CodeMlockFailed, Detail: d,
		Next: []contract.Next{{Argv: []string{"sitate", "doctor"}, RequiresHuman: true}}}
}
