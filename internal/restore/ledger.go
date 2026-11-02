package restore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 台帳(spec 4.5 の ledger/)の kind: dest_session の記録。

type resRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type destRef struct {
	At     string `json:"at"`
	KeyRef string `json:"key_ref"`
}

type sshRef struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
}

type teardownRec struct {
	Remaining     []resRef `json:"remaining"`
	FirstFailedAt string   `json:"first_failed_at"`
}

type ledgerRec struct {
	RecordID  string       `json:"record_id"`
	Kind      string       `json:"kind"`
	RunID     string       `json:"run_id"`
	Adapter   string       `json:"adapter"`
	Dest      destRef      `json:"dest"`
	Purpose   string       `json:"purpose"`
	SessionID string       `json:"session_id"`
	State     string       `json:"state"`
	Resources []resRef     `json:"resources"`
	SSH       sshRef       `json:"ssh"`
	HostKeyFP *string      `json:"host_key_fp"`
	BackupRef string       `json:"backup_ref"`
	Paths     []string     `json:"paths"`
	ExpiresAt *string      `json:"expires_at"`
	Teardown  *teardownRec `json:"teardown"`
}

func newRecordID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "l-" + hex.EncodeToString(b[:])
}

// readLedger は dest_session の記録(同じ record_id が重なれば後のもの)。
func readLedger(ctx context.Context, st contract.StateFiles) ([]ledgerRec, error) {
	raws, err := st.LedgerList(ctx)
	if err != nil {
		return nil, err
	}
	var out []ledgerRec
	idx := map[string]int{}
	for _, raw := range raws {
		var r ledgerRec
		if json.Unmarshal(raw, &r) != nil || r.Kind != "dest_session" {
			continue
		}
		if r.Resources == nil {
			r.Resources = []resRef{}
		}
		if i, ok := idx[r.RecordID]; ok {
			out[i] = r
			continue
		}
		idx[r.RecordID] = len(out)
		out = append(out, r)
	}
	return out, nil
}

// ledgerPut は記録を書く。LedgerAppend は同じ record_id を原子的に置き換える(state.lock の下)。
func ledgerPut(ctx context.Context, st contract.StateFiles, r ledgerRec) error {
	h, err := st.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	if r.Resources == nil {
		r.Resources = []resRef{}
	}
	b, _ := json.Marshal(r)
	return st.LedgerAppend(ctx, h, b)
}

func ledgerDelete(ctx context.Context, st contract.StateFiles, recordID string) error {
	h, err := st.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	return st.LedgerDelete(ctx, h, recordID)
}

// parseDestAt は ssh://<user>@<host>:<port> を接続先に分ける(ポートの省略は 22、IPv6 は [...])。
func parseDestAt(at string) (host string, port int, ok bool) {
	rest, found := strings.CutPrefix(at, "ssh://")
	if !found {
		return "", 0, false
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	port = 22
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			return "", 0, false
		}
		host, rest = rest[1:end], rest[end+1:]
		if p, f := strings.CutPrefix(rest, ":"); f {
			rest = p
		} else if rest != "" {
			return "", 0, false
		} else {
			return host, port, true
		}
	} else if i := strings.LastIndex(rest, ":"); i >= 0 {
		host, rest = rest[:i], rest[i+1:]
	} else {
		return rest, port, rest != ""
	}
	n := 0
	for _, ch := range rest {
		if ch < '0' || ch > '9' || n > 65535 {
			return "", 0, false
		}
		n = n*10 + int(ch-'0')
	}
	if n < 1 || n > 65535 || host == "" {
		return "", 0, false
	}
	return host, n, true
}
