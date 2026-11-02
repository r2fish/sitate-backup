package contract

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// スキーマ(JSON Schema 2020-12)は schema/ の下に置き、埋め込む。$id は https://sitate.invalid/schema/<相対パス>。
//
//go:embed schema
var schemaFS embed.FS

// SchemaBase はスキーマの $id の接頭辞。
const SchemaBase = "https://sitate.invalid/schema/"

var (
	compilerOnce sync.Once
	compiler     *jsonschema.Compiler
	compilerErr  error
)

func loadCompiler() (*jsonschema.Compiler, error) {
	compilerOnce.Do(func() {
		c := jsonschema.NewCompiler()
		c.DefaultDraft(jsonschema.Draft2020)
		compilerErr = fs.WalkDir(schemaFS, "schema", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
				return err
			}
			b, err := schemaFS.ReadFile(p)
			if err != nil {
				return err
			}
			var doc any
			if err := json.Unmarshal(b, &doc); err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			return c.AddResource(SchemaBase+strings.TrimPrefix(p, "schema/"), doc)
		})
		compiler = c
	})
	return compiler, compilerErr
}

// SchemaPaths は埋め込んだスキーマの相対パス(schema/ からの)の一覧を返す。
func SchemaPaths() ([]string, error) {
	var r []string
	err := fs.WalkDir(schemaFS, "schema", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".json") {
			r = append(r, strings.TrimPrefix(p, "schema/"))
		}
		return err
	})
	return r, err
}

var (
	compileMu sync.Mutex
	schemas   = map[string]*jsonschema.Schema{}
)

// compiled はスキーマをパスごとに 1 回だけコンパイルする。Compiler は並行に呼べない(Run の並行で Validate が重なる)。
func compiled(schemaPath string) (*jsonschema.Schema, error) {
	compileMu.Lock()
	defer compileMu.Unlock()
	if sch, ok := schemas[schemaPath]; ok {
		return sch, nil
	}
	c, err := loadCompiler()
	if err != nil {
		return nil, err
	}
	sch, err := c.Compile(SchemaBase + schemaPath)
	if err != nil {
		return nil, err
	}
	schemas[schemaPath] = sch
	return sch, nil
}

// Validate は JSON 文書を相対パスで指定したスキーマ(例 "cli/envelope.json")で検証する。
func Validate(schemaPath string, doc []byte) error {
	sch, err := compiled(schemaPath)
	if err != nil {
		return err
	}
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(doc)))
	if err != nil {
		return err
	}
	return sch.Validate(inst)
}
