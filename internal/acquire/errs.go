package acquire

// 誤りの組み立て(spec 3.1.3 の detail の形)と、依存の注入漏れの検査。

import (
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// mkErr は原因コードの表から区分と再試行の可否を引いて型付きの誤りを作る。detail は JSON にする値。
func mkErr(code contract.Code, detail any, next ...contract.Next) *contract.Error {
	info := contract.CodeTable[code]
	b, _ := json.Marshal(detail)
	if next == nil {
		next = []contract.Next{}
	}
	return &contract.Error{Code: code, Class: contract.ExitClass(info.Exit), Retryable: info.Retryable, Detail: b, Next: next}
}

func nextOf(why contract.Code, human bool, argv ...string) contract.Next {
	w := why
	return contract.Next{Argv: argv, RequiresHuman: human, Why: &w}
}

// errInternal は INTERNAL(component: acquire)。reason は exit_code / output / contract か空(null)。
func errInternal(reason string) *contract.Error {
	var r any
	if reason != "" {
		r = reason
	}
	return mkErr(contract.CodeInternal, map[string]any{"component": "acquire", "http_status": nil, "kind": nil, "name": nil, "reason": r})
}

func errArg(flag, reason string) *contract.Error {
	return mkErr(contract.CodeArgInvalid, map[string]any{"flag": flag, "reason": reason})
}

func errDefInvalid(pointer, reason string) *contract.Error {
	return mkErr(contract.CodeDefInvalid, map[string]any{"issues": []map[string]string{{"pointer": pointer, "reason": reason}}})
}

// errHostMismatch は source enroll の --host の <host> が定義の source.host と違うときの DEF_INVALID。
// next は定義を読み直す `sitate def show`(spec 3.1.3。D-0188)。
func errHostMismatch() *contract.Error {
	return mkErr(contract.CodeDefInvalid, map[string]any{"issues": []map[string]string{{"pointer": "/source/host", "reason": "invalid_value"}}},
		nextOf(contract.CodeDefInvalid, false, "sitate", "def", "show"))
}

func errUnreachable() *contract.Error {
	return mkErr(contract.CodeStorageUnreachable, map[string]any{"target": "source", "reason": "connect", "http_status": nil})
}

func errNotImplemented(feature string) *contract.Error {
	return mkErr(contract.CodeNotImplemented, map[string]any{"feature": feature})
}

// errSetup は SOURCE_SETUP_FAILED。detail の必須の項目をすべて持つ。
type setupDetail struct {
	Step, Reason string
	TargetID     *string
	Lacks        []string
	Unreadable   *int
	ServerVer    *int
	DumpVer      *int
	Missing      []string
}

func errSetup(d setupDetail) *contract.Error {
	if d.Lacks == nil {
		d.Lacks = []string{}
	}
	if d.Missing == nil {
		d.Missing = []string{}
	}
	var reason any
	if d.Reason != "" {
		reason = d.Reason
	}
	return mkErr(contract.CodeSourceSetupFailed, map[string]any{"step": d.Step, "target_id": d.TargetID, "reason": reason, "lacks": d.Lacks,
		"unreadable_tables": d.Unreadable, "server_version_num": d.ServerVer, "pg_dump_version_num": d.DumpVer, "missing": d.Missing},
		nextOf(contract.CodeSourceSetupFailed, d.Step == "connect" || d.Step == "sudo", "source", "enroll"))
}

func errSurveyFailed(step, reason string) *contract.Error {
	human := true
	return mkErr(contract.CodeSourceSurveyFailed, map[string]any{"step": step, "reason": reason},
		nextOf(contract.CodeSourceSurveyFailed, human, "source", "survey"))
}

// errHostKey は SOURCE_HOSTKEY_MISMATCH。expected が空なら null(照らす値が無い)。
func errHostKey(expected, seen string) *contract.Error {
	var e any
	if expected != "" {
		e = expected
	}
	return mkErr(contract.CodeSourceHostkeyMismatch, map[string]any{"expected_fp": e, "seen_fp": seen, "session_id": nil},
		nextOf(contract.CodeSourceHostkeyMismatch, expected != "", "source", "survey"))
}

// ctxErr は取り消された文脈を RUN_ABORTED にする(spec 3.1.1 の規則 11)。取り消されていなければ nil。
func ctxErr(ctx interface{ Err() error }) *contract.Error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	reason := "signal"
	if err.Error() == "context deadline exceeded" {
		reason = "deadline"
	}
	return mkErr(contract.CodeRunAborted, map[string]any{"reason": reason})
}

// need は注入漏れを INTERNAL(component: acquire)にする(契約: 依存の無い骨格は panic しない)。
func need(deps ...any) error {
	for _, d := range deps {
		if isNil(d) {
			return errInternal("")
		}
	}
	return nil
}

func isNil(v any) bool { return v == nil || nilIface(v) }
