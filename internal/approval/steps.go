package approval

// 適用の操作の列(spec 3.1.2.11「適用の操作の列」、3.1.2.9 の表の最後の列)と、適用時に要る秘密の判定。

import (
	"encoding/json"
	"slices"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 操作の列の要素の種類。
const (
	stepEnroll = "enroll"
	stepDefine = "define"
	stepRun    = "run"
)

func step(kind, entry string, args json.RawMessage) contract.OpStep {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return contract.OpStep{Kind: kind, Entry: entry, Args: args}
}

type planInfo struct {
	NeedsSecret *bool `json:"needs_secret"`
}

// needsEnroll は restore-change・source-change が登録(enroll)を要するか。plan.needs_secret が無ければ要するとして扱う(登録を省くのは cli が示したときだけ)。
func needsEnroll(rec changeRec) bool {
	var p planInfo
	_ = json.Unmarshal(rec.Plan, &p)
	return p.NeedsSecret == nil || *p.NeedsSecret
}

// runEntry は define 以外の種別の run の entry(3.1.2 のサブコマンドの名前か、部品の入口の名前)。
var runEntry = map[string]string{
	"storage-init":           "init-storage",
	"read-key-issue":         "storage issue-read-key",
	"restore-elsewhere":      "restore",
	"restore-session-extend": "restore",
	"restore-dest-release":   "restore-dest release",
	"key-rotate":             "key-rotate",
	"key-rotate-waive":       "key-rotate-waive",
	"phrase-reissue":         "phrase-reissue",
	"approval-key-change":    "approval-key-change",
	"storage-read-extra":     "restore",
	"hold-release":           "hold-release",
	"adapter-add":            "adapter-add",
}

// fullSteps は変更の操作の列。args は記録の args から作る。
func fullSteps(rec changeRec) []contract.OpStep {
	args := rec.Args
	switch rec.Op {
	case "restore-change":
		if needsEnroll(rec) {
			return []contract.OpStep{step(stepEnroll, "restore-dest enroll", args), step(stepDefine, "define", args)}
		}
	case "source-change":
		if needsEnroll(rec) {
			return []contract.OpStep{step(stepEnroll, "source enroll", args), step(stepDefine, "define", args)}
		}
	}
	if slices.Contains(defineOnly, rec.Op) || kinds[rec.Op].section != "" {
		return []contract.OpStep{step(stepDefine, "define", args)}
	}
	if e, ok := runEntry[rec.Op]; ok {
		return []contract.OpStep{step(stepRun, e, args)}
	}
	return nil
}

func isDefineOnly(rec changeRec) bool {
	fs := fullSteps(rec)
	return len(fs) == 1 && fs[0].Kind == stepDefine
}

// phraseOps はフレーズを出す変更(--secret-fd を要する。3.1.4)。
var phraseOps = []string{"phrase-reissue", "key-rotate"}

// missingSecret は適用時に要る秘密(3.1.2.11 の原因コードと分岐 1・2)が呼び出しに無いとき、waiting[] の reason を返す。
func missingSecret(rec changeRec, c contract.Call) string {
	has := func(flag string) bool { return len(c.Args[flag]) > 0 }
	switch rec.Op {
	case "storage-init", "read-key-issue":
		if !has("admin-ref") {
			return string(contract.CodeHumanSecretRequired)
		}
	case "restore-elsewhere":
		if !has("host-ref") {
			return string(contract.CodeHumanSecretRequired)
		}
	case "restore-change":
		if needsEnroll(rec) && !hasRestoreSecret(rec, c) {
			return string(contract.CodeHumanSecretRequired)
		}
	case "source-change":
		if needsEnroll(rec) && !has("source-admin-ref") {
			return string(contract.CodeHumanSecretRequired)
		}
	}
	if slices.Contains(phraseOps, rec.Op) && !c.SecretOut {
		return string(contract.CodeSecretFdRequired)
	}
	return ""
}

// requiresSecretArgs は秘密を渡す apply が --id を必須とすることの判定(3.1.2.11 の引数)。
func requiresSecretArgs(c contract.Call) bool {
	for _, k := range []string{"admin-ref", "source-admin-ref", "host-ref"} {
		if len(c.Args[k]) > 0 {
			return true
		}
	}
	return len(c.Secrets) > 0
}

// hasRestoreSecret は restore-change の登録に要る復元先の資格情報(提案の dest.key_ref の参照名の値)が --secrets-stdin にあるか。
func hasRestoreSecret(rec changeRec, c contract.Call) bool {
	var args struct {
		Restore struct {
			Dest struct {
				KeyRef string `json:"key_ref"`
			} `json:"dest"`
		} `json:"restore"`
	}
	_ = json.Unmarshal(rec.Args, &args)
	if ref := args.Restore.Dest.KeyRef; ref != "" {
		_, ok := c.Secrets[ref]
		return ok
	}
	return len(c.Secrets) > 0
}
