package cli

// 実行の排他(spec 3.1.1 の規則 1〜3・7): run.lock の flock と run/marker.json。ロックを取るのは cli だけ(作業指示書 1 章)。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Lease は contract.Lease の実物。Dir は状態ディレクトリ。MaxDuration は実行の上限(ゼロなら contract.RunMaxDuration)。
type Lease struct {
	Dir         string
	MaxDuration time.Duration

	mu sync.Mutex
	f  *os.File
}

var _ contract.Lease = (*Lease)(nil)

// flockFn は flock(2) の呼び出し口。ロックの基盤の失敗を試験で注入するために差し替える。
var flockFn = syscall.Flock

// keepAlive は取ったロックの記述子をプロセスの終了まで到達可能に保つ(GC が閉じるとロックが解ける。規則 2)。
var (
	keepMu    sync.Mutex
	keepAlive []*os.File
)

// Acquire は contract.Lease を満たす。run.lock(O_CREAT、close-on-exec。消さない)へ非待機の排他の flock。
// 空いていなければ (false, nil)(LOCKED への写しは呼び出し側)。持ち主の再取得は同じ記述子を保つ。基盤の失敗は LOCK_UNAVAILABLE。
func (l *Lease) Acquire(context.Context) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		return true, nil
	}
	if err := os.MkdirAll(l.Dir, 0o700); err != nil {
		return false, lockUnavailable(err)
	}
	f, err := os.OpenFile(filepath.Join(l.Dir, "run.lock"), os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return false, lockUnavailable(err)
	}
	if err := flockFn(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, nil
		}
		return false, lockUnavailable(err)
	}
	l.f = f
	keepMu.Lock()
	keepAlive = append(keepAlive, f)
	keepMu.Unlock()
	return true, nil
}

// lockUnavailable はロックの基盤の失敗(EACCES、ENOLCK、対応しないファイルシステム)を LOCK_UNAVAILABLE にする(規則 19)。
func lockUnavailable(err error) *contract.Error {
	reason := "unsupported_fs"
	switch {
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM), errors.Is(err, os.ErrPermission):
		reason = "eacces"
	case errors.Is(err, syscall.ENOLCK):
		reason = "enolck"
	}
	return newErr(contract.CodeLockUnavailable, map[string]any{"reason": reason, "fs_type": nil})
}

// WriteMarker は contract.Lease を満たす。印はロックの持ち主だけが書く(規則 7)。前の印(無ければ空)を返し、
// run/ の一時ファイルに書いて fsync → rename → run/ の fsync で置き換える。
func (l *Lease) WriteMarker(_ context.Context, marker []byte) ([]byte, error) {
	l.mu.Lock()
	held := l.f != nil
	l.mu.Unlock()
	if !held {
		return nil, internalErr()
	}
	dir := filepath.Join(l.Dir, "run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, internalErr()
	}
	prev, err := os.ReadFile(filepath.Join(dir, "marker.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, internalErr()
	}
	if err := writeFileAtomic(filepath.Join(dir, "marker.json"), marker); err != nil {
		return nil, internalErr()
	}
	return prev, nil
}

// Deadline は contract.Lease を満たす。開始から上限(規則 10)で取り消される文脈を返す。
func (l *Lease) Deadline(ctx context.Context) (context.Context, context.CancelFunc) {
	d := l.MaxDuration
	if d <= 0 {
		d = contract.RunMaxDuration
	}
	return context.WithTimeout(ctx, d)
}
