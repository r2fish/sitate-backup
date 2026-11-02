package restore

import (
	"bytes"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// dumpName は dump エントリの固定名(spec 3.1.2.4 のダンプ)。
func dumpName(format string) (string, bool) {
	switch format {
	case "custom":
		return "db.dump", true
	case "globals":
		return "globals.sql", true
	}
	return "", false
}

// relPath は検査済みのエントリの `<to>` からの相対パスの成分。
func relPath(e entry) []string {
	b, _ := e.pathBytes()
	return strings.Split(string(b), "/")
}

// validateEntries は置くエントリのすべてのパスを書き出しの前に検査する(spec 3.1.2.4 の「パスの拒否」)。
// 先頭の `/`、`.`・`..`・空の成分、NUL、対象 id で始まらない、dump で固定名と違う、未知の type、同じパスの重複、
// シンボリックリンクかファイルの下に置くエントリ(リンクの先へ書かないため)は INTERNAL(component: restore)。
func validateEntries(entries []entry) error {
	kinds := map[string]string{}
	for _, e := range entries {
		b, ok := e.pathBytes()
		if !ok || len(b) == 0 || b[0] == '/' || bytes.IndexByte(b, 0) >= 0 {
			return internalErr("contract")
		}
		comps := strings.Split(string(b), "/")
		for _, c := range comps {
			if c == "" || c == "." || c == ".." {
				return internalErr("contract")
			}
		}
		if e.T == "" || comps[0] != e.T {
			return internalErr("contract")
		}
		switch e.Type {
		case "file", "dir":
		case "symlink":
			if _, ok := e.pathBytesTarget(); !ok {
				return internalErr("contract")
			}
		case "dump":
			name, ok := dumpName(e.Format)
			if !ok || len(comps) != 2 || comps[1] != name {
				return internalErr("contract")
			}
		default:
			return internalErr("contract")
		}
		if _, dup := kinds[string(b)]; dup {
			return internalErr("contract")
		}
		kinds[string(b)] = e.Type
	}
	for p := range kinds {
		comps := strings.Split(p, "/")
		for i := 1; i < len(comps); i++ {
			if k, ok := kinds[strings.Join(comps[:i], "/")]; ok && k != "dir" {
				return internalErr("contract")
			}
		}
	}
	return nil
}

var _ = contract.CodeInternal
