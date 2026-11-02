package harness

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// DumpFileName は dump の format の固定名を返す。
func DumpFileName(format string) (string, bool) {
	switch format {
	case contract.DumpFormatCustom:
		return contract.DumpFileCustom, true
	case contract.DumpFormatGlobals:
		return contract.DumpFileGlobals, true
	}
	return "", false
}

var targetIDRe = regexp.MustCompile(`^t[1-9][0-9]{0,3}$`)

// MaterializePath はエントリの type・path(バイト列)・dump の format を検査し、<to> からの相対の置き場を返す
// (spec 3.1.2.4 の「パスの拒否」)。先頭の / ・. ・.. ・空の成分・対象 id で始まらない・dump の固定名違いは拒否。
func MaterializePath(typ string, path []byte, format string) (string, error) {
	if len(path) == 0 || path[0] == '/' || !StreamPathOK(path) || string(path) == "." {
		return "", fmt.Errorf("path が形に合わない")
	}
	parts := bytes.SplitN(path, []byte("/"), 2)
	if !targetIDRe.Match(parts[0]) {
		return "", fmt.Errorf("対象 id で始まらない")
	}
	if typ == "dump" {
		name, ok := DumpFileName(format)
		if !ok || len(parts) != 2 || string(parts[1]) != name {
			return "", fmt.Errorf("dump の固定名と違う")
		}
	}
	return string(path), nil
}

// 受け口の selftest の connect_error(spec 3.3)。LC_ALL=C の psql のエラーの文で分ける。順に当てて最初の一致を採る。
var connectErrorRules = []struct {
	name string
	re   *regexp.Regexp
}{
	{"connect_privilege", regexp.MustCompile(`User does not have CONNECT privilege`)},
	{"auth", regexp.MustCompile(`(?i)authentication failed|no pg_hba\.conf entry|no password supplied`)},
	{"socket", regexp.MustCompile(`No such file or directory|Connection refused|Is the server running locally`)},
}

// ClassifyConnectError は psql の標準エラーから connect_error の値(socket / auth / connect_privilege / other)を返す。
func ClassifyConnectError(stderr string) string {
	for _, r := range connectErrorRules {
		if r.re.MatchString(stderr) {
			return r.name
		}
	}
	return "other"
}

// ValidateReadBatchInput は read-batch の標準入力のパス一覧の検査(spec 3.3 の「read-batch の入力」)。
// 空のパス・/ で始まる・.・..・空の要素を含む・重複があれば誤り(受け口は何も出さず 126)。空の一覧は誤りにしない。
func ValidateReadBatchInput(paths [][]byte) error {
	seen := map[string]bool{}
	for _, p := range paths {
		if len(p) == 0 || p[0] == '/' || string(p) == "." || !StreamPathOK(p) || seen[string(p)] {
			return fmt.Errorf("read-batch の入力が不正: %q", p)
		}
		seen[string(p)] = true
	}
	return nil
}

// SplitNUL は NUL で終わる列を分ける(空の列は空)。最後が NUL で終わらなければ誤り。
func SplitNUL(b []byte) ([][]byte, error) {
	if len(b) == 0 {
		return nil, nil
	}
	if b[len(b)-1] != 0 {
		return nil, fmt.Errorf("NUL で終わっていない")
	}
	return bytes.Split(b[:len(b)-1], []byte{0}), nil
}

// ParseReadBatchTail は tar の終端の 2 ブロックの直後(r の現在位置)から、tar が record 単位に満たすゼロ埋めを読み飛ばし、
// 区切りの行と変化の一覧を読む(--blocking-factor を受け口の実装者が忘れて既定の 20 ブロックの record でも通る。区切りの行は非 NUL で始まる)。
func ParseReadBatchTail(r io.Reader) ([][]byte, error) {
	br := bufio.NewReader(r)
	for {
		b, err := br.Peek(1)
		if err != nil {
			return nil, fmt.Errorf("区切りの行が無い")
		}
		if b[0] != 0 {
			break
		}
		_, _ = br.ReadByte()
	}
	line, err := br.ReadString('\n')
	if err != nil || line != contract.ReadBatchChangedMarker {
		return nil, fmt.Errorf("区切りの行が無い")
	}
	rest, _ := io.ReadAll(br)
	l, err := SplitNUL(rest)
	if err != nil {
		return nil, err
	}
	return l, ValidateReadBatchInput(l)
}
