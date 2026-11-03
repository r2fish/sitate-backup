//go:build mainpath

package mainpath

// 主経路の第 2 段(作業指示書 6 章): 実物の sitate(make build-test の試験ビルド)を、受入試験の環境で、root でない sitate ユーザーとして
// 管理用 SSH の強制コマンド gate 経由で実行し、init から restore-test の入口までを通す。
// 実物: sitate のバイナリ・gate・SSH(sshd と x/crypto/ssh)・systemd/cron のタイマー・MinIO・rootless の podman・署名の検証(試験の鍵)。
// 偽物(代役): 保管先準備アダプタ minio と、置き場の b2 の差し替え(B2 の API を呼ばない代わり。test/mainpath/provminio)。

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

type s3Env struct{ endpoint, bucket, wk, ws, rk, rs string }

func loadS3(t *testing.T) s3Env {
	t.Helper()
	return s3Env{accEnv(t, "SITATE_TEST_S3_ENDPOINT"), accEnv(t, "SITATE_TEST_S3_BUCKET"), accEnv(t, "SITATE_TEST_S3_WRITE_ACCESS_KEY_ID"),
		accEnv(t, "SITATE_TEST_S3_WRITE_SECRET_ACCESS_KEY"), accEnv(t, "SITATE_TEST_S3_READ_ACCESS_KEY_ID"), accEnv(t, "SITATE_TEST_S3_READ_SECRET_ACCESS_KEY")}
}

// s2 は第 2 段の世界: 3 つのホストと gate の呼び出し口。
type s2 struct {
	t                 *testing.T
	src, bak, dst     *host
	g                 *gateCLI
	s3                s3Env
	bundle            string
	approvalPub       string
	phrase            string
	repoID, keyringID string
	approvalPriv      ed25519.PrivateKey // 承認鍵の秘密鍵(第 3 段の承認の署名。第 2 段は使わない)
	notifyTo          string             // 空なら電子メールの宛先(第 2 段)。第 3 段は Webhook の参照名
	lastMarker        string             // 第 3 段: 直近の世代の元データに入れた平文の目印(復元先に平文が残らないことの確認に使う)
	backupIP          string             // 元サーバー・復元先から見たバックアップサーバーのアドレス(SSH の送信元)
}

func (w *s2) definition() string {
	d := map[string]any{"version": 1,
		"source":  map[string]any{"host": accEnv(w.t, "SITATE_ACCEPT_SOURCE_HOST"), "targets": []any{map[string]any{"id": "t1", "files": "/srv/shopapp"}}},
		"storage": []any{map[string]any{"endpoint": w.s3.endpoint, "bucket": w.s3.bucket, "key_ref": "storage", "prefix": "sitate/"}},
		"restore": map[string]any{"adapter": "ssh-host", "dest": map[string]any{"at": w.destAt(), "key_ref": "restore-init"}},
		"notify":  map[string]any{"to": []string{w.notifyDest()}}}
	b, _ := json.Marshal(d)
	return string(b)
}

func (w *s2) notifyDest() string {
	if w.notifyTo != "" {
		return w.notifyTo
	}
	return "owner@example.invalid"
}

func newS2(t *testing.T) *s2 {
	t.Helper()
	w := &s2{t: t, s3: loadS3(t)}
	w.src, w.bak, w.dst = newHost(t, "source"), newHost(t, "backup"), newHost(t, "dest")
	w.backupIP = strings.TrimSpace(w.bak.root(`ip -4 route get "$(getent hosts ` + accEnv(t, "SITATE_ACCEPT_DEST_HOST") + ` | cut -d' ' -f1 | head -1)" | sed -n 's/.* src \([0-9.]*\).*/\1/p'`))
	if w.backupIP == "" {
		t.Fatal("バックアップサーバーのアドレスを得られない")
	}
	w.bundle = buildAndSign(t)
	w.g = bootstrapBackup(t, w.bak, w.bundle)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	w.approvalPriv = priv
	sp, _ := ssh.NewPublicKey(pub)
	w.approvalPub = base64.StdEncoding.EncodeToString(sp.Marshal())
	return w
}

// provisionAdapter は保管先準備アダプタの試験用の代役(test/mainpath/provminio)の置き場の名前。init-storage・bucket check に --adapter で渡す。
const provisionAdapter = "minio"

// storageAdmin は init-storage の事業者の管理用鍵(--admin-ref admin の値)を --secrets-stdin の 1 行にする。代役のアダプタは中身を見ない(枠が届くことだけ確かめる)。
func storageAdmin() string {
	b, _ := json.Marshal(map[string]any{"admin": map[string]string{"key_id": "mp-admin-id", "secret": "mp-admin-secret-value"}})
	return string(b) + "\n"
}

// initAndStorage は仕様 3.8 の順(手順 4 def validate → 手順 5 init-storage → init → phrase verify → bucket check)を実物の順で通す。
//   - def validate は seal の前なので storage[0].key_ref と restore.dest.key_ref の未解決を pending_refs[] に返す(D-0160)。台本は秘密ファイルを置かない。
//   - init-storage は保管先準備アダプタ(代役 minio)で鍵を発行して秘密ファイルに保存する。init の前なので記録はマスター鍵がまだ無い(E3 の pre-master)。
//   - phrase verify の確定は、マスター鍵が keys/pending/master.json にある間に記録を書く(E3)。台本は master.json を写さない。
//   - bucket check は --key-ref を省き、候補の storage[0].key_ref を使う(D-0160)。
func (w *s2) initAndStorage() {
	t := w.t
	w.bak.provisionDouble(t, provisionAdapter, w.s3)
	// bucket check は --adapter を受けず、既定の保管先準備アダプタ b2 を呼ぶ(spec 3.1.2)。同梱の b2 は B2 の API にしか届かないので、置き場の b2 を代役に差し替える。
	// init-storage は --adapter minio で呼び、--adapter が鍵の発行まで効くこと(E2)を通す。
	w.bak.provisionDouble(t, "b2", w.s3)
	o := w.g.call(w.definition()+"\n", "def", "validate", "--file", "-").ok(t, "def validate")
	var dv struct {
		Pending []string `json:"pending_refs"`
	}
	_ = json.Unmarshal(o.Env.Result, &dv)
	sort.Strings(dv.Pending)
	if strings.Join(dv.Pending, ",") != "/restore/dest/key_ref,/storage/0/key_ref" {
		t.Fatalf("def validate の pending_refs = %v", dv.Pending)
	}
	is := w.g.call(storageAdmin(), "init-storage", "--endpoint", w.s3.endpoint, "--bucket", w.s3.bucket, "--prefix", "sitate/", "--adapter", provisionAdapter, "--admin-ref", "admin", "--secrets-stdin").ok(t, "init-storage")
	t.Logf("init-storage: %.600s", is.Env.Result)
	checkPermissions(t, "init-storage", is.resultMap(t))
	o = w.g.call("", "init", "--approval-pubkey", w.approvalPub, "--secret-fd", "3").ok(t, "init")
	var rec struct {
		Words []string `json:"words"`
	}
	if o.Secret == nil || json.Unmarshal(o.Secret, &rec) != nil || len(rec.Words) == 0 {
		t.Fatalf("init がフレーズの秘密のレコードを先頭に出していない")
	}
	w.phrase = strings.Join(rec.Words, " ")
	r := o.resultMap(t)
	w.repoID, _ = r["repo_id"].(string)
	w.keyringID, _ = r["keyring_id"].(string)
	w.g.call(w.phrase+"\n", "phrase", "verify", "--phrase-stdin").ok(t, "phrase verify")
	o = w.g.call("", "bucket", "check").ok(t, "bucket check")
	t.Logf("bucket check: %s", o.Env.Result)
	checkPermissions(t, "bucket check", o.resultMap(t))
}

// checkPermissions は結果の permissions[] を判定する: 要素があり、すべて actual が expected と一致し、untested が無い。
// 注意: capabilities の列(権限の読み取りで判定する操作)は、代役のアダプタ(provminio)が環境のポリシーと同じ固定の表を返すので、MinIO の鍵の実際の権限を読んだ値ではない。
// 試す操作(版指定なしの削除、保持の短縮、リーガルホールドなど)は実物の MinIO に実際に試している。
func checkPermissions(t *testing.T, what string, r map[string]any) {
	t.Helper()
	ps, _ := r["permissions"].([]any)
	if len(ps) == 0 {
		t.Fatalf("%s: permissions[] が空", what)
	}
	for _, p := range ps {
		m, _ := p.(map[string]any)
		if m["actual"] == "untested" || m["actual"] != m["expected"] {
			t.Errorf("%s: %v/%v の expected=%v actual=%v", what, m["op"], m["key_role"], m["expected"], m["actual"])
		}
	}
	t.Logf("%s: permissions[] %d 要素すべてで expected = actual(untested なし)", what, len(ps))
}

// verifyJournal は、確定の後の実行を重ねた最後に、状態ディレクトリの journal/ の連番が 1 から切れずに並び、
// すべての項目が保管先に写った記録(state.json の objects[] の journal/<名前>)を持つことを確かめる(E3: 確定の前の項目は確定の後に写る)。
func (w *s2) verifyJournal() {
	t := w.t
	names := strings.Fields(w.bak.root("ls " + stateDir + "/journal | grep -E '^[0-9]{12}-[0-9a-f]{64}$' | sort"))
	if len(names) == 0 {
		t.Fatal("journal/ に項目が無い")
	}
	var st struct {
		Objects []struct {
			Key string `json:"key"`
		} `json:"objects"`
	}
	if err := json.Unmarshal([]byte(w.bak.root("cat "+stateDir+"/state.json")), &st); err != nil {
		t.Fatalf("state.json: %v", err)
	}
	have := map[string]bool{}
	for _, o := range st.Objects {
		have[o.Key] = true
	}
	for i, n := range names {
		if got := n[:12]; got != fmt.Sprintf("%012d", i+1) {
			t.Fatalf("journal/ の連番が 1 から切れずに並んでいない: %d 番目が %s", i+1, got)
		}
		if !have["journal/"+n] {
			t.Errorf("journal/%s が保管先に写った記録(objects[])に無い", n)
		}
	}
	t.Logf("journal/: %d 項目が連番で並び、すべて保管先に写っている", len(names))
}

// sourceAdmin は元サーバーの管理用資格情報(OpenSSH 形式の秘密鍵)を --secrets-stdin の 1 行にする。
func (w *s2) sourceAdmin() string {
	b, _ := json.Marshal(map[string]string{"admin": string(readF(w.t, accEnv(w.t, "SITATE_ACCEPT_ADMIN_KEY_FILE")))})
	return string(b) + "\n"
}

func (w *s2) sourceHostArg() string {
	return "ssh://" + adminUser(w.t) + "@" + accEnv(w.t, "SITATE_ACCEPT_SOURCE_HOST")
}

// enrollSource は source survey(元サーバーのホスト鍵を固定する)→ source enroll(source-setup を実行し、受け口を置く)。
// from= の値は SSH_CONNECTION から核が観測する。台本は、元サーバーの authorized_keys の from= が、元サーバーから見たバックアップサーバーのアドレスと
// 一致することを、実際の SSH の先で確かめる。
func (w *s2) enrollSource() {
	t := w.t
	o := w.g.call(w.sourceAdmin(), "source", "survey", "--host", w.sourceHostArg(), "--admin-ref", "admin", "--secrets-stdin").ok(t, "source survey")
	t.Logf("source survey: %.300s", o.Env.Result)
	o = w.g.call(w.sourceAdmin(), "source", "enroll", "--host", w.sourceHostArg(), "--admin-ref", "admin", "--secrets-stdin").ok(t, "source enroll")
	t.Logf("source enroll: %.600s", o.Env.Result)
	// 核が固定した元サーバーのホスト鍵の指紋が、環境の起動時に記録した値と一致する(実際の SSH で見えた鍵)。
	if got, _ := o.resultMap(t)["host_key_fp"].(string); got != accEnv(t, "SITATE_ACCEPT_SOURCE_HOST_KEY_FP") {
		t.Fatalf("source enroll の host_key_fp が起動時の記録と違う: %s", got)
	}
	line := w.src.root("grep -rh 'sitate-recv' /root/.ssh/authorized_keys /home/*/.ssh/authorized_keys /var/lib/sitate*/.ssh/authorized_keys /etc/ssh/ 2>/dev/null | head -3 || true")
	t.Logf("元サーバーの受け口の行(鍵は伏せる): %s", redactKeys(line))
	if !strings.Contains(line, `from="`+w.backupIP+`"`) {
		t.Fatalf("受け口の from= が、元サーバーから見たバックアップサーバーのアドレス %s と違う: %s", w.backupIP, redactKeys(line))
	}
}

func redactKeys(s string) string {
	var out []string
	for _, f := range strings.Fields(s) {
		if len(f) > 60 && !strings.Contains(f, "=") {
			f = "<鍵>"
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

func TestStage2MainPath(t *testing.T) {
	if os.Getenv("SITATE_ACCEPT_NAME") == "" {
		t.Fatal("受入試験の環境の変数が無い(ACCEPT=<NAME> を付けて実行)")
	}
	w := newS2(t)
	w.initAndStorage()
	w.verifyTimers()
	w.enrollSource()
	// seal(propose seal → 承認の署名・通知の確認語・遅延 → apply)は台本の外(承認の署名は台本の外)。seal の前のまま進める:
	// backup・restore-test は seal の前も止まらず、候補の定義で動く(spec 3.1.3 の NOT_SEALED、D-0025 の 5)。
	ref := w.backupAndList()
	w.restoreAndCompare(ref)
	w.destEnrollAndRestoreTest(ref)
	w.verifyJournal()
}

// backupAndList は backup(gate 経由。実物の SSH で元サーバーから pull し、rootless の podman の容器で取得する)→ list。世代の ref を返す。
func (w *s2) backupAndList() string {
	t := w.t
	o := w.g.call("", "backup").ok(t, "backup")
	t.Logf("backup: %.800s", o.Env.Result)
	r := o.resultMap(t)
	ref, _ := r["snapshot_ref"].(string)
	if ref == "" {
		t.Fatalf("backup の結果に snapshot_ref が無い: %s", o.Env.Result)
	}
	l := w.g.call("", "list").ok(t, "list")
	t.Logf("list: %.600s", l.Env.Result)
	if !strings.Contains(string(l.Env.Result), ref) {
		t.Fatalf("list に backup の世代 %s が無い", ref)
	}
	return ref
}

// restoreAndCompare は dest で restore し、元データと照合する。gate がバックアップサーバー自身への restore を拒むことも確かめる。
func (w *s2) restoreAndCompare(ref string) {
	t := w.t
	w.g.call("", "restore", "--backup", ref, "--to", "/var/tmp/restore-local").fails(t, "gate: バックアップサーバー自身への restore", "GATE_DENIED", 2)
	w.installCore()
	snap := w.snapshotKey(ref)
	o := w.restoreOnDest(ref, destRestoreRoot, snap).ok(t, "dest の restore")
	t.Logf("restore: %.600s", o.Env.Result)
	w.mustMatch("restore(ref 指定)", destRestoreRoot+"/t1")
}

func (w *s2) destAt() string {
	return "ssh://" + adminUser(w.t) + "@" + accEnv(w.t, "SITATE_ACCEPT_DEST_HOST") + ":" + accEnv(w.t, "SITATE_ACCEPT_DEST_PORT")
}

// destEnrollAndRestoreTest は restore-dest enroll(復元先 ssh-host の登録。dest の管理用アカウントの鍵を初期資格情報として標準入力で渡す)→
// restore-dest check → restore-test の入口。restore-test が通らない場合は、止まった場所を記録に残すため、結果を書いて先を試験の失敗にしない範囲で返す。
func (w *s2) destEnrollAndRestoreTest(ref string) {
	t := w.t
	o := w.g.call(w.sourceAdmin(), "restore-dest", "enroll", "--dest", w.destAt(), "--adapter", "ssh-host", "--admin-ref", "admin", "--secrets-stdin")
	o.ok(t, "restore-dest enroll")
	t.Logf("restore-dest enroll: %.600s", o.Env.Result)
	if got, _ := o.resultMap(t)["host_key_fp"].(string); got != accEnv(t, "SITATE_ACCEPT_DEST_HOST_KEY_FP") {
		t.Fatalf("restore-dest enroll の host_key_fp が起動時の記録と違う: %s", got)
	}
	c := w.g.call("", "restore-dest", "check").ok(t, "restore-dest check")
	t.Logf("restore-dest check: %.400s", c.Env.Result)
	// 手順 10: 実物の propose seal。ssh-host の restore.dest.key_ref は登録の記録で解決し、提案が作られる(承認の署名の先は台本の外。seal は未到達)。
	ps := w.g.call(w.definition()+"\n", "propose", "seal", "--file", "-").ok(t, "propose seal")
	if id, _ := ps.resultMap(t)["change_id"].(string); id == "" {
		t.Fatalf("propose seal が change_id を返さない: %s", ps.Env.Result)
	}
	t.Logf("propose seal: %.400s", ps.Env.Result)
	rt := w.g.call("", "restore-test").ok(t, "restore-test")
	var rr struct {
		RestoreOK  bool   `json:"restore_ok"`
		BackupRef  string `json:"backup_ref"`
		KeyringOK  bool   `json:"keyring_ok"`
		DefCopyOK  bool   `json:"def_copy_ok"`
		ConfigOK   bool   `json:"config_copy_ok"`
		Deferred   bool   `json:"deferred"`
		DurationS  int    `json:"duration_s"`
		EgressByte int64  `json:"egress_bytes"`
	}
	if err := json.Unmarshal(rt.Env.Result, &rr); err != nil {
		t.Fatalf("restore-test の結果: %v", err)
	}
	// config_copy_ok は、seal(apply の定義の確定)が config/<def_hash> を保管先へ書くため、seal を省いたこの台本では false になる。
	if !rr.RestoreOK || rr.BackupRef != ref || !rr.KeyringOK || !rr.DefCopyOK || rr.Deferred || rr.EgressByte <= 0 {
		t.Fatalf("restore-test の結果が期待と違う(期待: 直近の世代 %s、keyring_ok・def_copy_ok が真、送らない、保管先からの読み出し>0): %s", ref, rt.Env.Result)
	}
	t.Logf("restore-test: 世代 %s を dest で復元して照合(duration_s=%d、読み出し %d バイト、config_copy_ok=%v)", rr.BackupRef, rr.DurationS, rr.EgressByte, rr.ConfigOK)
	w.recordEnrollAndRemaining(rt) // D-0159: observed_from の記録と stop の remaining の実機の確かめ(stage3_remaining_test.go)
}
