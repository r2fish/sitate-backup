package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 完了確認の JSON Pointer が同じサブコマンドの結果スキーマに存在することの検査(作業指示書 3.2「手順書」)。
// 対応は results.json の $defs の名前で取る: サブコマンドの名前の空白と - を _ にした名前(def validate -> def_validate)。

const (
	schemaDir  = "internal/contract/schema/"
	schemaBase = "https://sitate.invalid/schema/"
	resultsRel = "cli/results.json"
)

// schemaSet は読み込んだスキーマ文書(相対パス -> 文書)。$ref の解決に使う。
type schemaSet struct {
	root string
	docs map[string]map[string]any
}

type schemaNode struct {
	file string
	node any
}

func defName(cmd string) string {
	return strings.NewReplacer(" ", "_", "-", "_").Replace(cmd)
}

func (s *schemaSet) doc(rel string) (map[string]any, error) {
	if d, ok := s.docs[rel]; ok {
		return d, nil
	}
	var d map[string]any
	if err := readJSON(s.root, schemaDir+rel, &d); err != nil {
		return nil, err
	}
	s.docs[rel] = d
	return d, nil
}

// deref は $ref を辿った先のノードを返す(外部の文書は schemaBase からの相対で読む)。
func (s *schemaSet) deref(cur schemaNode, ref string) (schemaNode, error) {
	file, frag, _ := strings.Cut(ref, "#")
	if file == "" {
		file = cur.file
	} else {
		file = strings.TrimPrefix(file, schemaBase)
	}
	d, err := s.doc(file)
	if err != nil {
		return schemaNode{}, err
	}
	var n any = d
	for _, p := range strings.Split(strings.TrimPrefix(frag, "/"), "/") {
		if p == "" {
			continue
		}
		m, ok := n.(map[string]any)
		if !ok || m[p] == nil {
			return schemaNode{}, fmt.Errorf("$ref が解決できない: %s", ref)
		}
		n = m[p]
	}
	return schemaNode{file: filepath.ToSlash(file), node: n}, nil
}

// reach は節 segs がスキーマ n の下にあり得るかを返す。スキーマが何も縛らない場所(properties の無い object など)は有り得るとして通す。
// strict が真のとき(自分の properties を持つ節の allOf・anyOf・oneOf の枝を見るとき)は、値を縛らない枝は有り得るとしない。
func (s *schemaSet) reach(n schemaNode, segs []string, depth int, strict bool) (bool, error) {
	m, ok := n.node.(map[string]any)
	if !ok || depth > 64 {
		return !strict, nil
	}
	if ref, ok := m["$ref"].(string); ok {
		t, err := s.deref(n, ref)
		if err != nil {
			return false, err
		}
		return s.reach(t, segs, depth+1, strict)
	}
	if len(segs) == 0 {
		return true, nil
	}
	_, ownProps := m["properties"]
	_, ownItems := m["items"]
	own := ownProps || ownItems
	if own {
		if r, err := s.reachChild(n, m, segs, depth, strict); err != nil || r {
			return r, err
		}
	}
	for _, k := range []string{"anyOf", "oneOf", "allOf"} {
		if alts, ok := m[k].([]any); ok {
			for _, a := range alts {
				if r, err := s.reach(schemaNode{n.file, a}, segs, depth+1, strict || own); err != nil || r {
					return r, err
				}
			}
			return false, nil
		}
	}
	if own {
		return false, nil
	}
	return s.reachChild(n, m, segs, depth, strict)
}

func (s *schemaSet) reachChild(n schemaNode, m map[string]any, segs []string, depth int, strict bool) (bool, error) {
	seg, rest := segs[0], segs[1:]
	if props, ok := m["properties"].(map[string]any); ok {
		if c, ok := props[seg]; ok {
			return s.reach(schemaNode{n.file, c}, rest, depth+1, false)
		}
	}
	if it, ok := m["items"]; ok && (seg == "<i>" || isDigits(seg)) {
		return s.reach(schemaNode{n.file, it}, rest, depth+1, false)
	}
	switch ap := m["additionalProperties"].(type) {
	case map[string]any:
		return s.reach(schemaNode{n.file, ap}, rest, depth+1, false)
	case bool:
		return ap, nil
	}
	_, hasProps := m["properties"]
	_, hasItems := m["items"]
	return !strict && !hasProps && !hasItems && !scalarType(m["type"]), nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func scalarType(t any) bool {
	s, ok := t.(string)
	return ok && s != "object" && s != "array"
}

// checkResultPointer は /result の下の Pointer が cmd の結果スキーマにあることを検査する。問題があれば文を返す。
func (d *lintData) checkResultPointer(ptr, cmd string) string {
	if ptr != "/result" && !strings.HasPrefix(ptr, "/result/") {
		return ""
	}
	if d.schemas == nil {
		return ""
	}
	name := defName(cmd)
	res, err := d.schemas.deref(schemaNode{file: resultsRel}, "#/$defs/"+name)
	if err != nil {
		return fmt.Sprintf("結果スキーマ(%s)に %s の $defs が無い", resultsRel, name)
	}
	segs := strings.Split(strings.TrimPrefix(strings.TrimPrefix(ptr, "/result"), "/"), "/")
	if segs[0] == "" {
		segs = nil
	}
	ok, err := d.schemas.reach(res, segs, 0, false)
	if err != nil {
		return err.Error()
	}
	if !ok {
		return fmt.Sprintf("Pointer %s が %s の結果スキーマ(%s)に無い", ptr, cmd, name)
	}
	return ""
}

func newSchemaSet(root string) *schemaSet {
	_, err := os.Stat(filepath.Join(root, schemaDir, resultsRel))
	if err != nil {
		return nil
	}
	return &schemaSet{root: root, docs: map[string]map[string]any{}}
}
