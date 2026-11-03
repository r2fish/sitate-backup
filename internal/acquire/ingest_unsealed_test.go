package acquire

// 主経路の第 2 段で見つけた誤り E4 の試験: 取得は seal の前も止めない(spec 3.1.3 の NOT_SEALED、D-0025 の 5)。
// seal の前は承認済みの定義(def/approved.json)が無いので、候補の定義(def validate に通った最新の内容)で取得する。

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

// unsealedState は seal の前の状態: ReadDef は NOT_SEALED、ReadCandidate は候補を返す。
type unsealedState struct {
	*fake.StateFiles
	cand []byte
}

func (s *unsealedState) ReadDef(context.Context) ([]byte, string, error) {
	return nil, "", mkErr(contract.CodeNotSealed, map[string]any{})
}

func (s *unsealedState) ReadCandidate(context.Context) ([]byte, string, bool, error) {
	return s.cand, "", s.cand != nil, nil
}

func TestIngestBeforeSealUsesCandidateDefinition(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.a.State = &unsealedState{StateFiles: &fake.StateFiles{Items: map[string]json.RawMessage{"initialized": json.RawMessage("true"), "sealed": json.RawMessage("false"), "phrase_confirmed": json.RawMessage("true")}}, cand: []byte(ingestDef)}
	e.script(newFrames().dir(".").file("a.txt", []byte("hello")).end(false), okResult("ok"))
	r, err := e.run(t)
	if err != nil {
		t.Fatalf("seal の前の取得が止まった(候補の定義で取得するはず): %v", err)
	}
	if len(r.Targets) != 1 || r.Targets[0].Status != "ok" {
		t.Fatalf("対象の結果: %+v", r.Targets)
	}
}

// 候補も無いときは、従来どおり NOT_SEALED。
func TestIngestBeforeSealWithoutCandidateIsNotSealed(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.a.State = &unsealedState{StateFiles: &fake.StateFiles{Items: map[string]json.RawMessage{"initialized": json.RawMessage("true"), "phrase_confirmed": json.RawMessage("true")}}}
	_, err := e.run(t)
	if ce, ok := err.(*contract.Error); !ok || ce.Code != contract.CodeNotSealed {
		t.Fatalf("NOT_SEALED のはず: %v", err)
	}
}

// F3: 確定(phrase verify)を終えるまで取得を始めない(PHRASE_UNCONFIRMED)。seal の前も後も同じ。
func TestIngestRefusesBeforePhraseConfirmed(t *testing.T) {
	for _, sealed := range []string{"false", "true"} {
		e := newIngestEnv(t, ingestDef)
		e.a.State = &unsealedState{StateFiles: &fake.StateFiles{Items: map[string]json.RawMessage{"initialized": json.RawMessage("true"), "sealed": json.RawMessage(sealed)}}, cand: []byte(ingestDef)}
		_, err := e.run(t)
		if ce, ok := err.(*contract.Error); !ok || ce.Code != contract.CodePhraseUnconfirmed {
			t.Fatalf("sealed=%s: PHRASE_UNCONFIRMED のはず: %v", sealed, err)
		}
	}
}

// seal の後(sealed: true)に approved.json だけが失われた状態では、候補へ開かず NOT_SEALED のまま。
func TestIngestAfterSealDoesNotFallBackToCandidate(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.a.State = &unsealedState{StateFiles: &fake.StateFiles{Items: map[string]json.RawMessage{"initialized": json.RawMessage("true"), "sealed": json.RawMessage("true"), "phrase_confirmed": json.RawMessage("true")}}, cand: []byte(ingestDef)}
	_, err := e.run(t)
	if ce, ok := err.(*contract.Error); !ok || ce.Code != contract.CodeNotSealed {
		t.Fatalf("NOT_SEALED のはず: %v", err)
	}
}
