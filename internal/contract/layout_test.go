package contract

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// internal/ の 9 パッケージに doc.go があり、20 行以内で、package 句の前のコメントは doc.go だけにある(作業指示書 5.1 の完了条件 11)。
func TestDocGoLayout(t *testing.T) {
	for _, p := range []string{"adapter", "format", "store", "acquire", "approval", "restore", "keys", "cli", "state"} {
		dir := filepath.Join("..", p)
		b, err := os.ReadFile(filepath.Join(dir, "doc.go"))
		if err != nil {
			t.Errorf("%s: doc.go が無い", p)
			continue
		}
		if n := strings.Count(string(b), "\n"); n > 20 {
			t.Errorf("%s/doc.go が %d 行(上限 20)", p, n)
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ParseComments|parser.PackageClauseOnly)
			if err != nil {
				t.Fatal(err)
			}
			isDoc := filepath.Base(f) == "doc.go"
			if isDoc != (af.Doc != nil) {
				t.Errorf("%s: package 句の前のコメントは doc.go だけに置く(doc.go=%v, コメント=%v)", f, isDoc, af.Doc != nil)
			}
		}
	}
}

// 骨格の置き場(作業指示書 5.1 の完了条件 4 のうち、ファイルの存在で確かめられるもの)。
func TestSkeletonPathsExist(t *testing.T) {
	for _, p := range []string{
		"cmd/sitate/main.go", "adapters/acquire/files/main.go", "adapters/acquire/recv/embed.go", "adapters/acquire/postgresql/main.go",
		"adapters/check/entropy-ratio/main.go", "adapters/check/ransom-note/main.go", "adapters/check/image/README.md",
		"adapters/restore-dest/ssh-host/main.go", "adapters/notify/webhook/main.go", "adapters/provision/b2/main.go",
		"app/src/i18n/ja/catalog.json", "app/src/i18n/en/catalog.json", "app/src/kit/types.ts", "app/src/data/send-fields.json",
		"app/test/contract/tools.test.ts", "app/test/e2e/.gitkeep", "assets/manual/manual.md", "assets/rules/approval-card-format.md",
		"assets/models/models.json", "data/prices/doc.go", "docs/install.md", "test/acceptance/scripts/.gitkeep",
		"test/acceptance/records/README.md", "test/e2e/harness.go", "test/env/dev/Dockerfile", "test/env/state-snapshots/.gitignore",
		"Makefile", "README.md", "go.mod", "tools/lintsize/allowlist.txt", "tools/lintsize/main.go", "tools/lintinstall/main.go",
		"app/eslint.config.js", "app/src/i18n/keys.json", "app/src/i18n/check.ts", "app/src/tools/README.md", "app/test/contract/catalog.test.ts",
		"app/test/contract/assets.test.ts", "app/test/contract/kit-types.test.ts", "app/test/contract/send-fields.test.ts",
		"assets/rules/outgoing-detection.md", "assets/rules/internal-terms.md", "assets/rules/data-section-rules.md", "assets/rules/quote-check-forms.md",
		"internal/contract/schema/app/send-fields.json", "internal/contract/schema/cli/subcommands.json",
	} {
		if _, err := os.Stat(filepath.Join("..", "..", p)); err != nil {
			t.Errorf("骨格が無い: %s", p)
		}
	}
	for file, want := range map[string]string{".dockerignore": ".claude/", ".gitignore": "/.claude/worktrees/"} {
		b, err := os.ReadFile(filepath.Join("..", "..", file))
		if err != nil || !strings.Contains("\n"+string(b), "\n"+want+"\n") {
			t.Errorf("%s に %s の行が無い", file, want)
		}
	}
	if b, _ := os.ReadFile(filepath.Join("..", "..", ".gitignore")); !strings.Contains(string(b), "/test/env/state-snapshots/") {
		t.Error(".gitignore に test/env/state-snapshots/ の行が無い")
	}
}
