package approval

// 確認語の照合(spec 3.1.2.10「確認語の形と照合」、4.5「確認語の塩とハッシュ」)。確認語そのものは記録しない。

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type proofRow struct {
	ChangeID string `json:"change_id"`
	To       string `json:"to"`
	Salt     string `json:"word_salt"`
	Hash     string `json:"word_hash"`
}

// normalizeWord は照合の前の正規化: NFKC、小文字化、前後の空白の除去、続く空白(全角・タブを含む)を半角空白 1 つに。
func normalizeWord(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(norm.NFKC.String(s))), " ")
}

func wordHash(salt []byte, word string) string {
	h := sha256.Sum256(append(append([]byte(nil), salt...), []byte(normalizeWord(word))...))
	return contract.HashPrefix + hex.EncodeToString(h[:])
}

func (r proofRow) matches(word string) bool {
	salt, err := base64.StdEncoding.DecodeString(r.Salt)
	if err != nil || len(r.Hash) != len(contract.HashPrefix)+64 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(wordHash(salt, word)), []byte(r.Hash)) == 1
}

// requiredProofs は確認語を入れさせる人向けの宛先と、送った確認語の行。
// seal は渡した定義の notify.to の全部と最後の notify test の行、notify-add・notify-change は plan.proof_required[] とその提案の作成時の行。
func (a *Approval) requiredProofs(ctx context.Context, rec changeRec) ([]string, []proofRow, error) {
	var ns struct {
		TestProofs   []proofRow `json:"test_proofs"`
		ChangeProofs []proofRow `json:"change_proofs"`
	}
	if _, err := a.getItem(ctx, "notify", &ns); err != nil {
		return nil, nil, err
	}
	switch rec.Op {
	case "seal":
		var args struct {
			Definition struct {
				Notify struct {
					To []string `json:"to"`
				} `json:"notify"`
			} `json:"definition"`
		}
		_ = json.Unmarshal(rec.Args, &args)
		return args.Definition.Notify.To, ns.TestProofs, nil
	case "notify-add", "notify-change":
		var plan struct {
			Required []string `json:"proof_required"`
		}
		_ = json.Unmarshal(rec.Plan, &plan)
		var rows []proofRow
		for _, r := range ns.ChangeProofs {
			if r.ChangeID == rec.ChangeID {
				rows = append(rows, r)
			}
		}
		return plan.Required, rows, nil
	}
	return nil, nil, nil
}

func needsProof(op string) bool { return op == "seal" || op == "notify-add" || op == "notify-change" }

// checkProofs は --notify-proof-stdin の 1 行の JSON(宛先 → 確認語)を照合する。すべて一致したとき true。
// 照合する宛先が無ければ確認語を受けず nil を返す。キーの欠け・対象でない宛先のキー・一致しない語はどれも NOTIFY_PROOF_MISMATCH(mismatched[])。
func (a *Approval) checkProofs(ctx context.Context, rec changeRec, stdin string) (*bool, error) {
	need, rows, err := a.requiredProofs(ctx, rec)
	if err != nil {
		return nil, err
	}
	if len(need) == 0 {
		if rec.Op == "seal" { // 照合の対象が無い seal は、人向けの宛先が機能するかの確認を欠く
			return nil, apiErr(contract.CodeNotifyProofMismatch, map[string]any{"mismatched": []string{}})
		}
		return nil, nil
	}
	given := map[string]string{}
	if stdin != "" && json.Unmarshal([]byte(stdin), &given) != nil {
		return nil, apiErr(contract.CodeNotifyProofMismatch, map[string]any{"mismatched": need})
	}
	bad := map[string]bool{}
	for _, to := range need {
		w, ok := given[to]
		if !ok || !rowMatches(rows, to, w) {
			bad[to] = true
		}
	}
	for to := range given {
		if !slices.Contains(need, to) {
			bad[to] = true
		}
	}
	if len(bad) > 0 {
		list := make([]string, 0, len(bad))
		for k := range bad {
			list = append(list, k)
		}
		slices.Sort(list)
		return nil, apiErr(contract.CodeNotifyProofMismatch, map[string]any{"mismatched": list})
	}
	ok := true
	return &ok, nil
}

func rowMatches(rows []proofRow, to, word string) bool {
	for _, r := range rows {
		if r.To == to && r.matches(word) {
			return true
		}
	}
	return false
}
