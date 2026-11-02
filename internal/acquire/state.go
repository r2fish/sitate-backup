package acquire

// 状態ファイル(state.json の項目)の読み書きの小さな道具。書くときは state.lock を取る(spec 4.5)。

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func getBool(ctx context.Context, st contract.StateFiles, item string) (bool, error) {
	raw, err := st.Get(ctx, item)
	if err != nil {
		return false, err
	}
	var b bool
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &b)
	}
	return b, nil
}

type hostKeyRec struct {
	Role      string  `json:"role"`
	Host      string  `json:"host"`
	HostKeyFP *string `json:"host_key_fp"`
	SessionID *string `json:"session_id"`
}

type seenRec struct {
	Host      string `json:"host"`
	HostKeyFP string `json:"host_key_fp"`
	At        string `json:"at"`
}

func getList[T any](ctx context.Context, st contract.StateFiles, item string) ([]T, error) {
	raw, err := st.Get(ctx, item)
	if err != nil {
		return nil, err
	}
	var l []T
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, errInternal("")
		}
	}
	return l, nil
}

// sourcePins は host の role: source の固定値と source_seen の指紋(あれば)。
func sourcePins(ctx context.Context, st contract.StateFiles, host string) (fixed, seen []string, err error) {
	hk, err := getList[hostKeyRec](ctx, st, "host_keys")
	if err != nil {
		return nil, nil, err
	}
	for _, h := range hk {
		if h.Role == "source" && h.Host == host && h.HostKeyFP != nil {
			fixed = append(fixed, *h.HostKeyFP)
		}
	}
	sn, err := getList[seenRec](ctx, st, "source_seen")
	if err != nil {
		return nil, nil, err
	}
	for _, s := range sn {
		if s.Host == host {
			seen = append(seen, s.HostKeyFP)
		}
	}
	return fixed, seen, nil
}

// readDef は定義を読む。seal の前は定義を保存した候補、後は承認済みの定義。
func readDef(ctx context.Context, st contract.StateFiles) ([]byte, error) {
	sealed, err := getBool(ctx, st, "sealed")
	if err != nil {
		return nil, err
	}
	if !sealed {
		d, _, found, err := st.ReadCandidate(ctx)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errDefInvalid("/source", "missing")
		}
		return d, nil
	}
	d, _, err := st.ReadDef(ctx)
	return d, err
}

func nowRFC3339(ctx context.Context, c contract.Clock) string {
	if c == nil {
		return time.Now().UTC().Format(time.RFC3339)
	}
	t, err := c.Now(ctx)
	if err != nil {
		t = time.Now()
	}
	return t.UTC().Format(time.RFC3339)
}

// withLock は state.lock を取って f を行う。
func withLock(ctx context.Context, st contract.StateFiles, f func(contract.StateLockHandle) error) error {
	h, err := st.Lock(ctx)
	if err != nil {
		return err
	}
	defer h.Release()
	return f(h)
}

// putSourceSeen は見えた指紋を、その接続先の source_seen[] の要素に置き換えて書く(他の接続先は残す)。
func putSourceSeen(ctx context.Context, st contract.StateFiles, clock contract.Clock, host, fp string) error {
	return withLock(ctx, st, func(h contract.StateLockHandle) error {
		old, err := getList[seenRec](ctx, st, "source_seen")
		if err != nil {
			return err
		}
		l := []seenRec{}
		for _, s := range old {
			if s.Host != host {
				l = append(l, s)
			}
		}
		l = append(l, seenRec{Host: host, HostKeyFP: fp, At: nowRFC3339(ctx, clock)})
		b, _ := json.Marshal(l)
		return st.Update(ctx, h, "source_seen", b)
	})
}

// adminSecret は --admin-ref の値(OpenSSH 形式の秘密鍵の文字列)を Call.Secrets か Secrets から引く。
func adminSecret(ctx context.Context, c contract.Call, sec contract.Secrets) (string, *contract.Secret, error) {
	refs := c.Args[contract.ArgAdminRef]
	if len(refs) != 1 || refs[0] == "" {
		return "", nil, errArg("--"+contract.ArgAdminRef, "missing")
	}
	ref := refs[0]
	raw, ok := c.Secrets[ref]
	if !ok {
		if sec == nil {
			return "", nil, errInternal("")
		}
		var err error
		if raw, err = sec.Get(ctx, ref); err != nil {
			return "", nil, err
		}
	}
	return ref, contract.NewSecret(secretKeyText(raw)), nil
}

// secretKeyText は秘密の JSON 値から鍵の文字列を取り出す(文字列か、private_key を持つ object)。
func secretKeyText(raw json.RawMessage) []byte {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []byte(s)
	}
	var o struct {
		PrivateKey string `json:"private_key"`
	}
	if json.Unmarshal(raw, &o) == nil && o.PrivateKey != "" {
		return []byte(o.PrivateKey)
	}
	return []byte(strings.TrimSpace(string(raw)))
}

func hostArg(c contract.Call) (hostSpec, error) {
	v := c.Args[contract.ArgHost]
	if len(v) != 1 {
		return hostSpec{}, errArg("--"+contract.ArgHost, "missing")
	}
	h, e := parseHostArg(v[0])
	if e != nil {
		return hostSpec{}, e
	}
	return h, nil
}
