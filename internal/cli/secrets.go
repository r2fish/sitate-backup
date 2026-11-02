package cli

// 秘密ファイル secrets/<参照名>.json(spec 4.5 の「参照名と秘密ファイル」)。ディレクトリ 0700、ファイル 0600。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/r2fish/sitate-backup/internal/contract"
)

var secretRefRe = regexp.MustCompile(`^[a-z0-9-]{1,27}(-next)?$`)

// Secrets は contract.Secrets の実物。Dir は状態ディレクトリ。
type Secrets struct{ Dir string }

var _ contract.Secrets = (*Secrets)(nil)

func (s *Secrets) file(ref string) (string, error) {
	if !secretRefRe.MatchString(ref) {
		return "", argErr("--ref", "format")
	}
	return filepath.Join(s.Dir, "secrets", ref+".json"), nil
}

// Exists は参照名が秘密ファイルとして存在するか(StateFiles.Refs。定義の unresolved_ref の検査)。形の不正な名前は無いものとして扱う。
func (s *Secrets) Exists(_ context.Context, ref string) (bool, error) {
	f, err := s.file(ref)
	if err != nil {
		return false, nil
	}
	_, err = os.Stat(f)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Get は contract.Secrets を満たす。無ければ SECRET_MISSING(detail.ref)。
func (s *Secrets) Get(_ context.Context, ref string) (json.RawMessage, error) {
	f, err := s.file(ref)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(f)
	if errors.Is(err, os.ErrNotExist) {
		return nil, newErr(contract.CodeSecretMissing, map[string]any{"ref": ref})
	}
	if err != nil || !json.Valid(b) {
		return nil, internalErr()
	}
	return json.RawMessage(b), nil
}

// Put は contract.Secrets を満たす。上書きは新しい値にする(一時ファイル + rename)。
func (s *Secrets) Put(_ context.Context, ref string, v json.RawMessage) error {
	f, err := s.file(ref)
	if err != nil {
		return err
	}
	if !json.Valid(v) {
		return argErr("--secrets-stdin", "format")
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return internalErr()
	}
	if err := writeFileAtomic(f, v); err != nil {
		return internalErr()
	}
	return nil
}

// Delete は contract.Secrets を満たす。無い参照名の削除は成功(冪等)。
func (s *Secrets) Delete(_ context.Context, ref string) error {
	f, err := s.file(ref)
	if err != nil {
		return err
	}
	if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
		return internalErr()
	}
	return nil
}
