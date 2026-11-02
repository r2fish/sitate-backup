package restore

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type mismatch struct {
	EntryRef string `json:"entry_ref"`
	Reason   string `json:"reason"`
}

// verifyFiles は期待(マニフェスト)と実際(復元先の報告)を件数・大きさ・内容ハッシュで照合する。
// files・bytes は期待の側、files_ok は一致した件数、files_failed は不一致(欠け・余分・大きさ・ハッシュ)の件数。
func verifyFiles(exp, act []contract.RestoreVerifyFile) (ok bool, files, bytes int64, filesOK int, mm []mismatch) {
	got := map[string]contract.RestoreVerifyFile{}
	for _, a := range act {
		got[a.EntryRef] = a
	}
	seen := map[string]bool{}
	mm = []mismatch{}
	for _, e := range exp {
		files++
		bytes += e.Size
		seen[e.EntryRef] = true
		a, found := got[e.EntryRef]
		switch {
		case !found:
			mm = append(mm, mismatch{e.EntryRef, contract.RestoreVerifyMissing})
		case a.Size != e.Size:
			mm = append(mm, mismatch{e.EntryRef, contract.RestoreVerifySize})
		case a.Hash != e.Hash:
			mm = append(mm, mismatch{e.EntryRef, contract.RestoreVerifyHash})
		default:
			filesOK++
		}
	}
	for _, a := range act {
		if !seen[a.EntryRef] {
			seen[a.EntryRef] = true
			mm = append(mm, mismatch{a.EntryRef, contract.RestoreVerifyUnexpected})
		}
	}
	return len(mm) == 0, files, bytes, filesOK, mm
}

// RestoreVerify は核に組み込みの照合(spec 3.5 の表の 9)。expected と actual は RestoreVerifyInput の JSON。
func (r *Restore) RestoreVerify(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var exp, act contract.RestoreVerifyInput
	if json.Unmarshal([]byte(arg(c, contract.ArgRestoreVerifyExpected)), &exp) != nil {
		return contract.Reply{}, argInvalid("--"+contract.ArgRestoreVerifyExpected, "format")
	}
	if json.Unmarshal([]byte(arg(c, contract.ArgRestoreVerifyActual)), &act) != nil && arg(c, contract.ArgRestoreVerifyActual) != "" {
		return contract.Reply{}, argInvalid("--"+contract.ArgRestoreVerifyActual, "format")
	}
	ok, files, bytes, filesOK, mm := verifyFiles(exp.Files, act.Files)
	return reply(map[string]any{"ok": ok, "files": files, "bytes": bytes, "files_ok": filesOK, "files_failed": len(mm), "mismatches": mm})
}
