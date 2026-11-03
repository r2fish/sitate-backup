package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 期待する値を、コマンドの封筒(JSON)に対して判定する。

func pointerValues(root any, ptr string) ([]any, error) {
	cur := []any{root}
	for _, seg := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
		var next []any
		for _, c := range cur {
			switch v := c.(type) {
			case map[string]any:
				if seg == "<i>" {
					return nil, fmt.Errorf("%s: 配列でない値に <i>", ptr)
				}
				x, ok := v[seg]
				if !ok {
					return nil, fmt.Errorf("%s: 欄 %s が結果に無い", ptr, seg)
				}
				next = append(next, x)
			case []any:
				if seg != "<i>" {
					return nil, fmt.Errorf("%s: 配列に欄名 %s", ptr, seg)
				}
				next = append(next, v...)
			default:
				return nil, fmt.Errorf("%s: %s の手前で値が行き止まり", ptr, seg)
			}
		}
		cur = next
	}
	return cur, nil
}

// DefScheduleRef は手順書の右辺 `<定義の schedule>`(D-0168)。DefaultSchedule は定義に schedule が無いときの既定(spec 4.1)。
const (
	DefScheduleRef  = "<定義の schedule>"
	DefaultSchedule = "02:30"
)

func canon(v any) string { b, _ := json.Marshal(v); return string(b) }

// rhs は右辺を値に直す。{{印}} は marks から、`/…` で始まる Pointer は同じ結果の別の欄から、
// それ以外は JSON として読み、読めなければ文字列とする。
func rhs(root any, val string, marks Marks, elem int, count int) (any, error) {
	if val == DefScheduleRef { // 手順 13: 直近に合格した def validate の normalized.schedule(marks の DEF_SCHEDULE)。無ければ既定(spec 4.1)
		raw, ok := marks["DEF_SCHEDULE"]
		if !ok {
			raw = json.RawMessage(`"` + DefaultSchedule + `"`)
		}
		var v any
		err := json.Unmarshal(raw, &v)
		return v, err
	}
	if strings.HasPrefix(val, "{{") && strings.HasSuffix(val, "}}") {
		raw, ok := marks[strings.TrimSuffix(strings.TrimPrefix(val, "{{"), "}}")]
		if !ok {
			return nil, fmt.Errorf("印 %s の値が与えられていない", val)
		}
		var v any
		return v, json.Unmarshal(raw, &v)
	}
	if strings.HasPrefix(val, "/") {
		vs, err := pointerValues(root, val)
		if err != nil {
			return nil, err
		}
		if len(vs) != count {
			return nil, fmt.Errorf("%s: 要素数が左辺と違う", val)
		}
		return vs[elem], nil
	}
	var v any
	if json.Unmarshal([]byte(val), &v) == nil {
		return v, nil
	}
	return val, nil
}

func contains(set []any, x any) bool {
	for _, s := range set {
		if canon(s) == canon(x) {
			return true
		}
	}
	return false
}

// Judge は 1 要素の期待する値を封筒の JSON(根は封筒)に照らす。成立しなければ理由を返す。
func Judge(envelope []byte, e Expect, marks Marks) error {
	var root any
	if err := json.Unmarshal(envelope, &root); err != nil {
		return fmt.Errorf("結果が JSON でない: %w", err)
	}
	vals, err := pointerValues(root, e.Pointer)
	if err != nil {
		return err
	}
	switch e.Op {
	case "=":
		for i, v := range vals {
			want, err := rhs(root, e.Value, marks, i, len(vals))
			if err != nil {
				return err
			}
			if canon(v) != canon(want) {
				return fmt.Errorf("%s = %s のはずが %s", e.Pointer, canon(want), canon(v))
			}
		}
	case "⊇", "⊆":
		want, err := rhs(root, e.Value, marks, 0, 1)
		if err != nil {
			return err
		}
		return judgeSet(e, vals, want)
	default:
		return fmt.Errorf("演算子 %s を知らない", e.Op)
	}
	return nil
}

func judgeSet(e Expect, vals []any, want any) error {
	set, ok := want.([]any)
	if !ok {
		return fmt.Errorf("%s の右辺が配列でない", e.Pointer)
	}
	if e.Op == "⊇" {
		// 左辺は配列そのもの(<i> を含まない Pointer)。vals は 1 要素で、その配列が set を含む。
		arr, ok := vals[0].([]any)
		if !ok || len(vals) != 1 {
			return fmt.Errorf("%s は配列でない", e.Pointer)
		}
		for _, w := range set {
			if !contains(arr, w) {
				return fmt.Errorf("%s に %s が無い", e.Pointer, canon(w))
			}
		}
		return nil
	}
	for _, v := range vals {
		if !contains(set, v) {
			return fmt.Errorf("%s の %s は許容一覧の外", e.Pointer, canon(v))
		}
	}
	return nil
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
