package approval

// 前提の状態のハッシュ(spec 3.1.2.9: {"op","repo_id","def_hash", 表の材料} の RFC 8785 直列化の SHA-256)。
// propose と、apply の各変更の開始時の再計算(3.6 の apply の (b))で同じ関数を使う。

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type preState struct {
	hash   string
	target json.RawMessage // 節を変える種別だけ {section, section_hash_before}。ほかは null
}

// sectionHash は正規化した定義のその節の値の SHA-256(`sha256:...`)。節が無ければ null のハッシュ。
func sectionHash(norm []byte, section string) (string, error) {
	raw := []byte("null")
	if len(norm) > 0 {
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(norm, &doc); err != nil {
			return "", err
		}
		if v, ok := doc[section]; ok {
			raw = v
		}
	}
	return contract.CanonicalHash(raw)
}

// currentDef は前提に使う定義: seal の後は承認済みの定義、seal の前は保存した候補(3.1.2.9)。
func (a *Approval) currentDef(ctx context.Context) (norm []byte, defHash string, err error) {
	var sealed bool
	if _, err = a.getItem(ctx, "sealed", &sealed); err != nil {
		return nil, "", err
	}
	if !sealed {
		n, h, found, cerr := a.State.ReadCandidate(ctx)
		if cerr != nil {
			return nil, "", cerr
		}
		if found {
			return n, h, nil
		}
	}
	return a.State.ReadDef(ctx)
}

func (a *Approval) precondition(ctx context.Context, op string, args map[string]json.RawMessage) (preState, error) {
	spec := kinds[op]
	var repoID *string
	if _, err := a.getItem(ctx, "repo_id", &repoID); err != nil {
		return preState{}, err
	}
	m := map[string]any{"op": op, "repo_id": repoID}
	st := preState{target: json.RawMessage(`null`)}
	norm, defHash, err := a.currentDef(ctx)
	if err != nil {
		return st, err
	}
	switch {
	case spec.section != "":
		sh, err := sectionHash(norm, spec.section)
		if err != nil {
			return st, err
		}
		m["section"], m["section_hash"], m["retention"] = spec.section, sh, nil
		st.target, _ = json.Marshal(map[string]string{"section": spec.section, "section_hash_before": sh})
	case !spec.noDefHash:
		m["def_hash"] = defHash
	}
	if op == "approval-key-change" {
		var k struct {
			FP string `json:"key_fp"`
		}
		_, _ = a.getItem(ctx, "approval_key", &k)
		m["approval_key_fp"] = k.FP
	}
	if a.Planner != nil {
		_, mat, perr := a.Planner(ctx, op, args)
		if perr != nil {
			return st, perr
		}
		for k, v := range mat {
			if k != "op" && k != "repo_id" && k != "def_hash" {
				m[k] = v
			}
		}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return st, err
	}
	st.hash, err = contract.CanonicalHash(raw)
	return st, err
}

// 適用が定義を変える種別と、前提が定義(def_hash)を含む種別(propose の plan.conflicts[]。3.6 の「複数の変更」)。
func changesDef(op string) bool {
	return slices.Contains(defineOnly, op) || kinds[op].section != ""
}

func dependsDef(op string) bool {
	s, ok := kinds[op]
	return ok && s.section == "" && !s.noDefHash
}

// conflict は、先に適用されると他方の前提が変わる組(同じ節を変える 2 つを含む)。
func conflict(a, b string) bool {
	sa, sb := kinds[a].section, kinds[b].section
	if sa != "" && sa == sb {
		return true
	}
	return (changesDef(a) && dependsDef(b)) || (changesDef(b) && dependsDef(a))
}
