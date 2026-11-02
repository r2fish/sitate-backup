// Command lintsize は Go ファイルの大きさを検査する(作業指示書 1 章「コードの大きさ」、3.1)。
//
// 数え方: 行は物理行(空行・コメントを含む)。関数の長さは func の開始行から閉じ括弧の行まで
// (中のクロージャは別に数えず、外側の関数に含める)。テストのファイル(_test.go)は 600、他は 400、関数は 60。
// 生成コード(ast.IsGenerated)と許可リストのパスは対象外。
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const (
	maxFile     = 400
	maxTestFile = 600
	maxFunc     = 60
)

func main() {
	os.Exit(run(os.Stdout, os.Stderr))
}

type sz struct {
	path string
	n    int
}

func run(out, errw *os.File) int {
	files, err := targets(os.Getenv("LINT_BASE"))
	if err != nil {
		fmt.Fprintln(errw, "lint-size:", err)
		return 2
	}
	allow := loadAllow("tools/lintsize/allowlist.txt")
	var sizes []sz
	bad := 0
	for _, f := range files {
		if !strings.HasSuffix(f, ".go") || allow[f] {
			continue
		}
		n, b, ok := checkFile(f, errw)
		bad += b
		if ok {
			sizes = append(sizes, sz{f, n})
		}
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i].n > sizes[j].n })
	fmt.Fprintln(out, "大きいファイルの上位 10:")
	for i := 0; i < len(sizes) && i < 10; i++ {
		fmt.Fprintf(out, "  %5d %s\n", sizes[i].n, sizes[i].path)
	}
	if bad > 0 {
		fmt.Fprintf(out, "lint-size: 不合格(%d 件)\n", bad)
		return 1
	}
	fmt.Fprintf(out, "lint-size: 合格(Go %d ファイル)\n", len(sizes))
	return 0
}

// checkFile は 1 ファイルを検査し、行数・違反の件数・数えたか(生成コードや読めないものは false)を返す。
func checkFile(f string, errw *os.File) (int, int, bool) {
	src, err := os.ReadFile(f)
	if err != nil {
		return 0, 0, false
	}
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, f, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		fmt.Fprintf(errw, "lint-size: %s: %v\n", f, err)
		return 0, 1, false
	}
	if ast.IsGenerated(af) {
		return 0, 0, false
	}
	bad := 0
	lines := strings.Count(string(src), "\n")
	if len(src) > 0 && src[len(src)-1] != '\n' {
		lines++
	}
	limit := maxFile
	if strings.HasSuffix(f, "_test.go") {
		limit = maxTestFile
	}
	if lines > limit {
		fmt.Fprintf(errw, "lint-size: %s: %d 行(上限 %d)\n", f, lines, limit)
		bad++
	}
	for _, d := range af.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		if n := fset.Position(fd.End()).Line - fset.Position(fd.Pos()).Line + 1; n > maxFunc {
			fmt.Fprintf(errw, "lint-size: %s: 関数 %s が %d 行(上限 %d)\n", f, fd.Name.Name, n, maxFunc)
			bad++
		}
	}
	return lines, bad, true
}

// targets は検査の対象を返す。HEAD が無いとき(最初の commit の前)は管理下と未追跡の全部、
// あるときは LINT_BASE からの git diff --name-only(必須)と未追跡のファイル(commit の前の受け入れで新しいファイルを見落とさない)。
func targets(base string) ([]string, error) {
	if exec.Command("git", "rev-parse", "--verify", "-q", "HEAD").Run() != nil {
		return gitLines("ls-files", "--cached", "--others", "--exclude-standard")
	}
	if base == "" {
		return nil, fmt.Errorf("LINT_BASE=<開始 commit> が要ります(HEAD があるとき)")
	}
	changed, err := gitLines("diff", "--name-only", base)
	if err != nil {
		return nil, err
	}
	untracked, err := gitLines("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	return append(changed, untracked...), nil
}

func gitLines(args ...string) ([]string, error) {
	b, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, err
	}
	var r []string
	for _, l := range strings.Split(string(b), "\n") {
		if l != "" {
			if _, err := os.Stat(l); err == nil {
				r = append(r, l)
			}
		}
	}
	return r, nil
}

func loadAllow(path string) map[string]bool {
	m := map[string]bool{}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		m[strings.SplitN(l, "\t", 2)[0]] = true
	}
	return m
}
