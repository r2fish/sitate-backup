package state

// 状態ディレクトリの基盤(spec 4.5): 0700/0600 の作成、一時ファイル + fsync + rename + ディレクトリの fsync の置き換え、state.lock の短い排他。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// StateFiles は contract.StateFiles の実物。依存は公開フィールドで受ける(契約試験が差し込む。消さない)。Dir は状態ディレクトリ(spec 4.5)。
// 以下の任意の項目は、ゼロなら固定値・定数表の値を使う(試験ビルドの cli が短い値を入れる)。
type StateFiles struct {
	Dir   string
	Codec contract.Codec
	KeyS  contract.KeyService
	Clock contract.Clock

	// RunID は一時ファイルの名前(`.tmp-<run_id>-<乱数>`。spec 4.5)に使う。空なら "none"。
	RunID string
	// LockWait は state.lock を取る 1 回の待ち(STATE_LOCK_WAIT)、LockRetry は再試行の間隔(STATE_LOCK_RETRY_INTERVAL)。
	LockWait, LockRetry time.Duration
	// Refs は参照名が秘密ファイルとして存在するかを返す(ValidateDef の unresolved_ref)。nil なら解決を検査しない。
	Refs func(ctx context.Context, ref string) (bool, error)
}

var _ contract.StateFiles = (*StateFiles)(nil)

// flockFn は flock(2) の呼び出し口。ロックの基盤の失敗を試験で注入するために差し替える。
var flockFn = syscall.Flock

type lockHandle struct {
	owner *StateFiles
	mu    sync.Mutex
	f     *os.File
}

// Release は contract.StateLockHandle を満たす。二重の解放は何もしない。
func (h *lockHandle) Release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.f != nil {
		_ = h.f.Close()
		h.f = nil
	}
}

func (h *lockHandle) held(s *StateFiles) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.f != nil && h.owner.Dir == s.Dir
}

// heldBy は held がこの StateFiles の state.lock を持つ有効なハンドルであることを確かめる。
func (s *StateFiles) heldBy(held contract.StateLockHandle) error {
	h, ok := held.(*lockHandle)
	if !ok || h == nil || !h.held(s) {
		return internalErr()
	}
	return nil
}

func (s *StateFiles) path(parts ...string) string {
	return filepath.Join(append([]string{s.Dir}, parts...)...)
}

// mkdirAll は無ければ 0700 でディレクトリを作る(spec 4.5)。
func mkdirAll(dir string) error { return os.MkdirAll(dir, 0o700) }

// Lock は contract.StateFiles を満たす。state.lock(O_CREAT。消さない)に非待機の flock を LockRetry 間隔で繰り返し、
// LockWait か ctx の短い方までで取れなければ LOCKED(detail.lock: state)。基盤の失敗は LOCK_UNAVAILABLE。
func (s *StateFiles) Lock(ctx context.Context) (contract.StateLockHandle, error) {
	if err := mkdirAll(s.Dir); err != nil {
		return nil, lockUnavailable(err)
	}
	f, err := os.OpenFile(s.path("state.lock"), os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, lockUnavailable(err)
	}
	wait, retry := s.LockWait, s.LockRetry
	if wait <= 0 {
		wait = contract.StateLockWait
	}
	if retry <= 0 {
		retry = contract.StateLockRetryInterval
	}
	deadline := time.Now().Add(wait)
	for {
		err := flockFn(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &lockHandle{owner: s, f: f}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, lockUnavailable(err)
		}
		if time.Now().Add(retry).After(deadline) || ctx.Err() != nil {
			_ = f.Close()
			return nil, lockedErr()
		}
		select {
		case <-ctx.Done():
		case <-time.After(retry):
		}
	}
}

// lockUnavailable はロックの基盤の失敗(EACCES、ENOLCK、対応しないファイルシステム)を LOCK_UNAVAILABLE にする(spec 3.1.1 の規則 19)。
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

// writeAtomic は同じディレクトリの一時ファイル(O_EXCL、0600)に書いて fsync し、rename で置き換えてディレクトリを fsync する(spec 4.5)。
func (s *StateFiles) writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := mkdirAll(dir); err != nil {
		return err
	}
	var r [4]byte
	_, _ = rand.Read(r[:])
	id := s.RunID
	if id == "" {
		id = "none"
	}
	tmp := filepath.Join(dir, ".tmp-"+id+"-"+hex.EncodeToString(r[:]))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
