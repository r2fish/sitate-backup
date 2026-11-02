package acquire

// 契約試験: source enroll が合格するまでの経路と (4)(記録)(spec 3.3「source enroll の順」と「selftest[] の項目」、4.5 の host_keys・source_seen・source_clusters・receiver_hash)。
// 試験の中の元サーバー(同じ偽物が管理アクセスの root と pull 用の sitate-ro の両方を受ける。実物は同じホスト)で、
// 台本が `sh -s` に流れ、続けて pull 用鍵の受け口に自己診断を実行し、合格したときだけ記録を書くことを確かめる。
// 仕様に無く試験が決めたもの: pull 用の接続のポート(source.host にポートの欄が無いため、偽物を 22 と管理アクセスのポートの両方に置いて、どちらでも通す)、
// 受け口の中身は台本の中の最も長い単一引用符の文字列(3.3「入力」の「単一引用符の文字列として埋め込み」)として hash を計算する。

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// longestQuoted は台本の単一引用符の文字列(' は '\” で書く)のうち最も長いものの中身。
func longestQuoted(s string) string {
	best, cur := "", []byte(nil)
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case !in && c == '\'':
			in, cur = true, cur[:0]
		case in && c == '\'':
			if strings.HasPrefix(s[i:], `'\''`) {
				cur = append(cur, '\'')
				i += 3
			} else {
				in = false
				if len(cur) > len(best) {
					best = string(cur)
				}
			}
		case in:
			cur = append(cur, c)
		}
	}
	return best
}

func shaOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(h[:])
}

// enrollWorld は管理アクセスと pull 用の両方を受ける偽の元サーバー。leak* は受け口が拒否すべきものを通す。
type enrollWorld struct {
	src                       *fakeSource
	setup                     string
	lines                     int
	match                     bool
	badHash                   bool
	leakUnlisted, leakOutside bool
	pullExecs                 []execRec
}

func (w *enrollWorld) reply(t *testing.T) func(execRec) (string, int) {
	return func(r execRec) (string, int) {
		if r.User != "sitate-ro" {
			if strings.HasPrefix(r.Stdin, contract.EmbedVarScope+"=") {
				return dbOnlyDoc(t, true), 0
			}
			w.setup = r.Stdin
			return `{"step":"prereq","target_id":null,"result":"unchanged","reason":null,"missing":null}` + "\n" +
				`{"step":"done","target_id":null,"result":"created","reason":null,"missing":null}` + "\n", 0
		}
		w.pullExecs = append(w.pullExecs, r)
		f := strings.Fields(r.Cmd)
		switch f[0] {
		case "selftest":
			h := shaOf(longestQuoted(w.setup))
			if w.badHash {
				h = checkHash
			}
			return selftestDoc(w.lines, w.match, h, "ok"), 0
		case "list":
			if len(f) < 2 || f[1] != "t1" {
				if w.leakUnlisted {
					return "", 0
				}
				return "", 126
			}
			return "", 0
		case "read-batch":
			for _, p := range strings.Split(r.Stdin, "\x00") {
				if strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
					if w.leakOutside {
						return "", 0
					}
					return "", 126
				}
			}
			return "", 0
		case "survey", "dump-pg", "dump-pg-globals":
			return "{}", 0
		}
		if w.leakUnlisted {
			return "", 0
		}
		return "", 126
	}
}

func newEnrollWorld(t *testing.T) *enrollWorld {
	w := &enrollWorld{lines: 1, match: true}
	w.src = startSource22(t, w.reply(t)) // 22 に置く
	srcMu.Lock()
	srcByPt[w.src.Port] = w.src // 管理アクセスのポートにも置く
	srcMu.Unlock()
	return w
}

func runEnroll(t *testing.T, w *enrollWorld) (contract.Reply, error, *fake.StateFiles, *fake.SecretStore, string) {
	a, st := newSrcAcquire(enrollItems(map[string]json.RawMessage{"source_seen": seenItem("127.0.0.1", w.src.FP)}))
	sec := &fake.SecretStore{M: map[string]json.RawMessage{}}
	a.Secrets = sec
	key := newAdminKey(t)
	r, err := a.SourceEnroll(ctx, sourceCall(enrollWords, w.src.URL("root"), key))
	return r, err, st, sec, key
}

type enrollItem struct {
	Name     string  `json:"name"`
	TargetID *string `json:"target_id"`
	OK       bool    `json:"ok"`
	Code     *string `json:"code"`
}

// 合格まで: 調査(db_only)→ 台本(クラスタ・pull 用公開鍵を埋め込む)→ sitate-ro の自己診断。結果と記録 (4)。
func TestContractAcquireSourceEnrollSuccess(t *testing.T) {
	_, perr := shapeAcquire().SourceEnroll(ctx, contract.Call{Words: enrollWords})
	harness.Implemented(t, perr)
	w := newEnrollWorld(t)
	r, err, st, sec, adminKey := runEnroll(t, w)
	harness.Shape(t, enrollWords, r, err)
	var res struct {
		HostKeyFP, ReceiverHash string       `json:"-"`
		FP                      string       `json:"host_key_fp"`
		Hash                    string       `json:"receiver_hash"`
		Selftest                []enrollItem `json:"selftest"`
		Targets                 []struct {
			ID, Kind string
			Root     *string
		} `json:"targets"`
	}
	harness.Result(t, r, &res)
	if res.FP != w.src.FP {
		t.Errorf("host_key_fp は見えた指紋: %q", res.FP)
	}
	if res.Hash != shaOf(longestQuoted(w.setup)) || w.setup == "" {
		t.Errorf("receiver_hash は台本に埋め込んだ受け口の中身の sha256: %q", res.Hash)
	}
	names := map[string]enrollItem{}
	for _, it := range res.Selftest {
		names[it.Name] = it
		if !it.OK || it.Code != nil {
			t.Errorf("合格なのに項目が落ちた: %+v", it)
		}
	}
	for _, n := range []string{"from_restricted", "single_key_line", "receiver_hash", "unlisted_op_denied", "outside_path_denied", "targets_match"} {
		if _, ok := names[n]; !ok {
			t.Errorf("selftest[] に項目 %s が無い", n)
		}
	}
	if it := names["targets_match"]; it.TargetID == nil || *it.TargetID != "t1" {
		t.Errorf("targets_match の target_id は t1: %+v", it)
	}
	if len(res.Targets) != 1 || res.Targets[0].ID != "t1" || res.Targets[0].Kind != "files" || res.Targets[0].Root == nil || *res.Targets[0].Root != "/srv/app" {
		t.Errorf("targets[] は t1・files・/srv/app: %+v", res.Targets)
	}
	// 実行の順と権限。
	ex := w.src.Execs()
	if len(ex) < 3 || ex[0].User != "root" || ex[1].User != "root" || ex[2].User != "sitate-ro" || ex[1].Cmd != "sh -s" {
		t.Fatalf("順は 調査(root)→ source-setup(root, sh -s)→ sitate-ro の自己診断: %+v", ex)
	}
	if len(w.pullExecs) == 0 || !strings.HasPrefix(w.pullExecs[0].Cmd, "selftest") && !strings.HasPrefix(w.pullExecs[1%len(w.pullExecs)].Cmd, "selftest") {
		t.Errorf("sitate-ro で selftest を実行する: %+v", w.pullExecs)
	}
	checkPullKey(t, w, sec, adminKey, ex)
	checkEnrollRecords(t, w, st, res.Hash)
}

// pull 用鍵: Secrets に置き、公開鍵を台本に埋め込み、秘密鍵と管理用鍵は元サーバーへ流さない。
func checkPullKey(t *testing.T, w *enrollWorld, sec *fake.SecretStore, adminKey string, ex []execRec) {
	if contract.SourcePullKeyRef != "sitate-pull" {
		t.Fatalf("pull 用鍵の参照名は予約名 sitate-pull(spec 4.5): %s", contract.SourcePullKeyRef)
	}
	var sk contract.SSHKeySecret
	if err := json.Unmarshal(sec.M[contract.SourcePullKeyRef], &sk); err != nil {
		t.Fatalf("pull 用鍵が Secrets(%s)に無い: %v", contract.SourcePullKeyRef, err)
	}
	pem := sk.PrivateKey
	sg, err2 := ssh.ParsePrivateKey([]byte(pem))
	if err2 != nil {
		t.Fatalf("pull 用鍵は OpenSSH 形式の秘密鍵(private_key): %v", err2)
	}
	if sk.Kind != contract.SecretKindSSHKey || sk.PublicKey != strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sg.PublicKey()))) || sk.Fp != ssh.FingerprintSHA256(sg.PublicKey()) {
		t.Errorf("kind: ssh_key・public_key・fp が秘密鍵と合わない: %+v", sk)
	}
	if pem == adminKey {
		t.Error("pull 用鍵は管理用鍵と別")
	}
	if !strings.Contains(w.setup, base64.StdEncoding.EncodeToString(sg.PublicKey().Marshal())) {
		t.Error("source-setup の台本に pull 用の公開鍵を埋め込む")
	}
	for _, e := range ex {
		if strings.Contains(e.Stdin, "PRIVATE KEY") {
			t.Error("秘密鍵を元サーバーへ流さない")
		}
	}
}

// (4) 記録。
func checkEnrollRecords(t *testing.T, w *enrollWorld, st *fake.StateFiles, resHash string) {
	var hk []struct {
		Role string `json:"role"`
		Host string `json:"host"`
		FP   string `json:"host_key_fp"`
	}
	_ = json.Unmarshal(st.Items["host_keys"], &hk)
	if len(hk) != 1 || hk[0].Role != "source" || hk[0].Host != "127.0.0.1" || hk[0].FP != w.src.FP {
		t.Errorf("host_keys に role: source の固定値を置く: %s", st.Items["host_keys"])
	}
	var seen []map[string]any
	_ = json.Unmarshal(st.Items["source_seen"], &seen)
	if len(seen) != 0 {
		t.Errorf("固定値を置いたら同じ接続先の source_seen を消す: %s", st.Items["source_seen"])
	}
	var rh string
	_ = json.Unmarshal(st.Items["receiver_hash"], &rh)
	if rh != resHash {
		t.Errorf("state の receiver_hash は結果と同じ: %q", rh)
	}
	var sc struct {
		Host     string `json:"host"`
		Clusters []struct {
			SocketDir string   `json:"socket_dir"`
			Port      int      `json:"port"`
			Major     int      `json:"major"`
			RawPaths  []string `json:"raw_paths"`
		} `json:"clusters"`
	}
	_ = json.Unmarshal(st.Items["source_clusters"], &sc)
	if sc.Host != "127.0.0.1" || len(sc.Clusters) != 1 || sc.Clusters[0].SocketDir != "/var/run/postgresql" || sc.Clusters[0].Port != 5432 || sc.Clusters[0].Major != 16 ||
		len(sc.Clusters[0].RawPaths) != 1 || sc.Clusters[0].RawPaths[0] != "/var/lib/postgresql/16/main" {
		t.Errorf("source_clusters は読んだクラスタの一覧: %s", st.Items["source_clusters"])
	}
}

// 自己診断が 1 つでも外れたら RECEIVER_SELFTEST_FAILED(failed[] にその項目)。合格していないので host_keys・source_clusters・receiver_hash を書かない。
func TestContractAcquireSourceEnrollSelftestFails(t *testing.T) {
	_, perr := shapeAcquire().SourceEnroll(ctx, contract.Call{Words: enrollWords})
	harness.Implemented(t, perr)
	cases := []struct {
		name, item string
		set        func(*enrollWorld)
	}{
		{"行が置いた行と違う", "from_restricted", func(w *enrollWorld) { w.match = false }},
		{"鍵の行が 2 つ", "single_key_line", func(w *enrollWorld) { w.lines = 2 }},
		{"受け口の中身が違う", "receiver_hash", func(w *enrollWorld) { w.badHash = true }},
		{"許可リスト外の操作が通る", "unlisted_op_denied", func(w *enrollWorld) { w.leakUnlisted = true }},
		{"根の外のパスが通る", "outside_path_denied", func(w *enrollWorld) { w.leakOutside = true }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newEnrollWorld(t)
			c.set(w)
			_, err, st, _, _ := runEnroll(t, w)
			ce := harness.ExpectCode(t, err, contract.CodeReceiverSelftestFailed)
			errEnvelope(t, ce)
			var d struct {
				Failed []string `json:"failed"`
			}
			_ = json.Unmarshal(ce.Detail, &d)
			if len(d.Failed) != 1 || d.Failed[0] != c.item {
				t.Errorf("failed[] は [%s]: %s", c.item, ce.Detail)
			}
			for _, k := range []string{"host_keys", "source_clusters", "receiver_hash"} {
				if len(st.Items[k]) != 0 {
					t.Errorf("合格しなければ %s を書かない: %s", k, st.Items[k])
				}
			}
			if len(st.Items["source_seen"]) == 0 {
				t.Error("合格しなければ source_seen を消さない")
			}
		})
	}
}
