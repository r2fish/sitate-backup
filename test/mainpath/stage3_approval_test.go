//go:build mainpath

package mainpath

// 第 3 段: seal の承認(仕様 3.6、3.1.2.10、4.6)。承認鍵の署名・通知の確認語・遅延は実物の経路で通す。
// 承認カードを出すアプリの役は台本: 記録(pending --id)から canonical_hash を台本が自分で計算し直して署名する。

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// jcs は RFC 8785 の直列化(この記録の値域: 文字列・整数・真偽・null・配列・オブジェクト。浮動小数は使わない)。
func jcs(v any, buf *bytes.Buffer) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		buf.WriteString(strconv.FormatBool(x))
	case json.Number:
		if strings.ContainsAny(x.String(), ".eE") {
			return fmt.Errorf("浮動小数は扱わない: %s", x)
		}
		buf.WriteString(x.String())
	case string:
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		_ = e.Encode(x)
		buf.Write(bytes.TrimRight(b.Bytes(), "\n"))
	case []any:
		buf.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := jcs(e, buf); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		ks := make([]string, 0, len(x))
		for k := range x {
			ks = append(ks, k)
		}
		sort.Strings(ks) // UTF-16 の順。この記録のキーは ASCII だけ
		buf.WriteByte('{')
		for i, k := range ks {
			if i > 0 {
				buf.WriteByte(',')
			}
			_ = jcs(k, buf)
			buf.WriteByte(':')
			if err := jcs(x[k], buf); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("未対応の型 %T", v)
	}
	return nil
}

// pendingView は pending --id の変更 1 件(記録に state と not_before が足されたもの)。
type pendingView struct {
	raw       map[string]any
	State     string
	NotBefore any
	Hash      string // 記録の canonical_hash
	Computed  string // 台本が記録から計算し直した値
	Delay     int
}

func (w *s2) pendingView(id string) pendingView {
	t := w.t
	o := w.g.call("", "pending", "--id", id).ok(t, "pending")
	var r struct {
		Changes []json.RawMessage `json:"changes"`
	}
	if err := json.Unmarshal(o.Env.Result, &r); err != nil || len(r.Changes) != 1 {
		t.Fatalf("pending --id の結果が 1 件でない: %.300s", o.Env.Result)
	}
	dec := json.NewDecoder(bytes.NewReader(r.Changes[0]))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatal(err)
	}
	pv := pendingView{raw: m, State: fmt.Sprint(m["state"]), NotBefore: m["not_before"], Hash: fmt.Sprint(m["canonical_hash"])}
	if n, ok := m["delay_hours"].(json.Number); ok {
		pv.Delay, _ = strconv.Atoi(n.String())
	}
	rec := map[string]any{}
	for k, v := range m {
		if k != "state" && k != "not_before" && k != "canonical_hash" {
			rec[k] = v
		}
	}
	var b bytes.Buffer
	if err := jcs(rec, &b); err != nil {
		t.Fatalf("記録の直列化: %v", err)
	}
	sum := sha256.Sum256(b.Bytes())
	pv.Computed = "sha256:" + hex.EncodeToString(sum[:])
	return pv
}

// simNow は試験ビルドの核が読む模擬時刻(version の sim_time)。
func (w *s2) simNow() time.Time {
	o := w.g.call("", "version").ok(w.t, "version")
	var r struct {
		Sim string `json:"sim_time"`
	}
	_ = json.Unmarshal(o.Env.Result, &r)
	tm, err := time.Parse(time.RFC3339, r.Sim)
	if err != nil {
		w.t.Fatalf("version に sim_time が無い: %s", o.Env.Result)
	}
	return tm.UTC()
}

// setSimTime は共有の時刻を tm へ進める(戻さない)。test-tick --set-start は gate の許可リストに無い試験ビルドだけの入口なので、
// 管理アクセス(root)から sitate ユーザーとして直接呼ぶ(単位は起動しない。置くだけ)。
func (w *s2) setSimTime(tm time.Time) {
	w.bak.root(`runuser -u sitate -- env SITATE_SIM_TIME_FILE=/opt/sitate-test/shared/simtime SITATE_TEST_TRUST_PUBKEY_FILE=` + testTrustFn +
		` ` + sitateBin + ` test-tick --set-start ` + tm.UTC().Format(time.RFC3339))
}

// sign は承認鍵の署名(仕様 4.6: "sitate-approval/v1\n" + canonical_hash + "\n" + signed_at の Ed25519)。
func (w *s2) sign(hash, signedAt string) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(w.approvalPriv, []byte("sitate-approval/v1\n"+hash+"\n"+signedAt)))
}

// approveSeal は承認を行う。確認語は hook の宛先で最後に受けた notify.test から(人が通知で受け取って入力する役)。
func (w *s2) approveSeal(id string, word string) cliOut {
	pv := w.pendingView(id)
	if pv.Hash != pv.Computed {
		w.t.Fatalf("記録の canonical_hash を台本が計算し直した値と違う: 記録 %s、計算 %s", pv.Hash, pv.Computed)
	}
	signedAt := w.simNow().Format(time.RFC3339)
	proof, _ := json.Marshal(map[string]string{hookDest: word})
	return w.g.call(string(proof)+"\n", "approve", "--id", id, "--signature", w.sign(pv.Hash, signedAt), "--signed-at", signedAt, "--notify-proof-stdin")
}
