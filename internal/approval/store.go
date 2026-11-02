package approval

// pending/ の置き場(spec 4.5「pending/ の配置と連鎖」)。ファイル(状態ディレクトリ Dir の下)かメモリ(Dir が空。試験用)。
// 名前は pending/ からの相対("log.jsonl"、"changes/<id>.json"、"approvals/<id>.json")。名前は呼び出し側が検証済みの識別子から作る。

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	pendingDir = "pending"
	logName    = "log.jsonl"
	changesDir = "changes"
	approvDir  = "approvals"
)

type blobs interface {
	read(name string) ([]byte, bool, error)
	write(name string, b []byte) error // 置き換え(ファイルは一時ファイル + fsync + rename)
	appendTo(name string, b []byte) error
	list(dir string) ([]string, error)
}

// memBlobs は Dir が空のときの置き場(再起動で消える。結合では Dir を必ず与える)。
type memBlobs struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *memBlobs) read(name string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[name]
	return append([]byte(nil), b...), ok, nil
}

func (s *memBlobs) write(name string, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	s.m[name] = append([]byte(nil), b...)
	return nil
}

func (s *memBlobs) appendTo(name string, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	s.m[name] = append(s.m[name], b...)
	return nil
}

func (s *memBlobs) list(dir string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.m {
		if strings.HasPrefix(k, dir+"/") {
			out = append(out, strings.TrimPrefix(k, dir+"/"))
		}
	}
	sort.Strings(out)
	return out, nil
}

// fileBlobs は <Dir>/pending/ の下のファイル。
type fileBlobs struct{ root string }

func (s *fileBlobs) path(name string) string { return filepath.Join(s.root, filepath.FromSlash(name)) }

func (s *fileBlobs) read(name string) ([]byte, bool, error) {
	b, err := os.ReadFile(s.path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

func (s *fileBlobs) write(name string, b []byte) error {
	p := s.path(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (s *fileBlobs) appendTo(name string, b []byte) error {
	p := s.path(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func (s *fileBlobs) list(dir string) ([]string, error) {
	ents, err := os.ReadDir(s.path(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".tmp-") {
			out = append(out, e.Name())
		}
	}
	return out, err
}
