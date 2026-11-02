package approval

// 定義を受ける種別(seal・def-change・def-change-unlisted・checks-weaken)と承認鍵の変更の引数の検査。

import (
	"context"
	"encoding/json"

	"golang.org/x/crypto/ssh"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func (a *Approval) checkPubkey(ctx context.Context, args, plan map[string]json.RawMessage) error {
	var b64, kt string
	_ = json.Unmarshal(args["pubkey"], &b64)
	_ = json.Unmarshal(args["key_type"], &kt)
	if kt == "" {
		kt = "software"
	}
	if kt != "software" {
		return apiErr(contract.CodeNotImplemented, map[string]any{"feature": "approval-key-change " + kt})
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte("ssh-ed25519 " + b64))
	if err != nil || pub.Type() != ssh.KeyAlgoED25519 {
		return argErr("--pubkey", "format")
	}
	var cur struct {
		FP string `json:"key_fp"`
	}
	_, _ = a.getItem(ctx, "approval_key", &cur)
	plan["key_fp_before"] = mustJSON(cur.FP)
	plan["key_fp_after"] = mustJSON(ssh.FingerprintSHA256(pub))
	plan["key_type"] = mustJSON(kt)
	return nil
}

func (a *Approval) checkDefinition(ctx context.Context, kind string, file []string, args, plan map[string]json.RawMessage) error {
	if len(file) != 1 {
		return argErr("--file", "format")
	}
	if a.ReadDefinition == nil {
		return internalErr()
	}
	def, err := a.ReadDefinition(ctx, file[0])
	if err != nil {
		return err
	}
	norm, hash, issues, err := a.State.ValidateDef(ctx, def, false)
	if err != nil {
		return err
	}
	if len(issues) > 0 && string(issues) != "[]" && string(issues) != "null" {
		return apiErr(contract.CodeDefInvalid, map[string]any{"issues": issues})
	}
	if !json.Valid(norm) {
		return internalErr()
	}
	if kind == "seal" {
		if err := a.checkSeal(ctx, hash); err != nil {
			return err
		}
	} else {
		if err := a.checkNotMixed(ctx, kind, norm); err != nil {
			return err
		}
	}
	args["definition"] = norm
	plan["def_hash_after"] = mustJSON(hash)
	return nil
}

// checkSeal は seal 済みなら ALREADY_SEALED、渡した内容の def_hash が保存した候補と違えば DEF_INVALID(next: def validate --file -。D-0101)。
func (a *Approval) checkSeal(ctx context.Context, hash string) error {
	var sealed bool
	if _, err := a.getItem(ctx, "sealed", &sealed); err != nil {
		return err
	}
	if sealed {
		return apiErr(contract.CodeAlreadySealed, nil)
	}
	_, candHash, found, err := a.State.ReadCandidate(ctx)
	if err != nil {
		return err
	}
	if !found || candHash != hash {
		e := apiErr(contract.CodeDefInvalid, map[string]any{"issues": []map[string]string{{"pointer": "", "reason": "invalid_value"}}})
		e.Next = []contract.Next{{Argv: []string{"sitate", "def", "validate", "--file", "-"}}}
		return e
	}
	return nil
}

// checkNotMixed は定義を受ける種別(seal を除く)の差分が、その種別の項目だけであることを確かめる(3.6、A6 (12))。
// 他の種別の項目・必須項目・表に無い項目が混じれば DEF_CHANGE_MIXED。遅延 0 の def-change で通知先・復元先・遅延などを替えさせない。
func (a *Approval) checkNotMixed(ctx context.Context, op string, after []byte) error {
	before, _, err := a.State.ReadDef(ctx)
	if err != nil {
		return err
	}
	return checkDefDiff(op, before, after)
}
