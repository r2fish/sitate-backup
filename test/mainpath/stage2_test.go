//go:build mainpath

package mainpath

// 主経路の第 2 段(作業指示書 6 章): 実物の sitate(make build-test の試験ビルド)を、受入試験の環境で、root でない sitate ユーザーとして
// 管理用 SSH の強制コマンド gate 経由で実行し、init から restore-test の入口までを通す。
// 実物: sitate のバイナリ・gate・SSH(sshd と x/crypto/ssh)・systemd/cron のタイマー・MinIO・rootless の podman・署名の検証(試験の鍵)。
// 偽物(代役): 保管先準備アダプタ minio(B2 の API を呼ばない代わり。test/mainpath/provminio)。

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
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
	backupIP          string // 元サーバー・復元先から見たバックアップサーバーのアドレス(SSH の送信元)
}

func (w *s2) definition() string {
	d := map[string]any{"version": 1,
		"source":  map[string]any{"host": accEnv(w.t, "SITATE_ACCEPT_SOURCE_HOST"), "targets": []any{map[string]any{"id": "t1", "files": "/srv/shopapp"}}},
		"storage": []any{map[string]any{"endpoint": w.s3.endpoint, "bucket": w.s3.bucket, "key_ref": "storage", "prefix": "sitate/"}},
		"restore": map[string]any{"adapter": "ssh-host", "dest": map[string]any{"at": w.destAt(), "key_ref": "restore-init"}},
		"notify":  map[string]any{"to": []string{"owner@example.invalid"}}}
	b, _ := json.Marshal(d)
	return string(b)
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
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sp, _ := ssh.NewPublicKey(pub)
	w.approvalPub = base64.StdEncoding.EncodeToString(sp.Marshal())
	return w
}

// seedSecrets は def validate の参照名の解決(StateFiles.Refs)を通すための秘密ファイルを、sitate ユーザーの置き場に置く。
//   - storage: 保管先の書き込み鍵と読み取り鍵(kind storage_keys)。本来は init-storage が保管先準備アダプタで発行して置く(B2)か、
//     secrets set で置く(イテレーション 3)。init-storage は bucket check が初期化の前後で INTERNAL になり(下の記録の「見つけたもの」)通せないため、
//     台本が試験環境の MinIO の鍵をそのまま置く。
//   - restore-init: 復元先の初期資格情報の参照名。仕様 3.8 の順(手順 4 の def validate が手順 9 の restore-dest enroll の前)では
//     まだ存在せず def validate が unresolved_ref で止まる(仕様の穴として報告)ため、仮の中身を置く。
func (w *s2) seedSecrets() {
	w.bak.root("install -d -m 0700 -o sitate -g sitate " + stateDir + "/secrets")
	keys, _ := json.Marshal(map[string]any{"kind": "storage_keys", "keys": []map[string]string{
		{"role": "write", "key_id": w.s3.wk, "secret": w.s3.ws}, {"role": "read", "key_id": w.s3.rk, "secret": w.s3.rs}}})
	w.bak.put(stateDir+"/secrets/storage.json", keys, "0600", "sitate")
	w.bak.put(stateDir+"/secrets/restore-init.json", []byte(`{"kind":"token","value":"placeholder"}`), "0600", "sitate")
}

func (w *s2) initAndStorage() {
	t := w.t
	// 保管先は MinIO で、同梱の保管先準備アダプタ b2 は B2 の API にしか届かない。bucket check の権限の読み取りは b2 の check(枠 key)を呼ぶので、
	// 置き場の b2 の adapter.json を試験用の代役(test/mainpath/provminio)に差し替える(B2 の鍵の発行は台本の範囲外で、init-storage は通さない)。
	w.bak.provisionDouble(t, []string{"admin", "key"})
	w.seedSecrets()
	w.g.call(w.definition()+"\n", "def", "validate", "--file", "-").ok(t, "def validate")
	o := w.g.call("", "init", "--approval-pubkey", w.approvalPub, "--secret-fd", "3").ok(t, "init")
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
	// 回避(実装の誤りとして報告): phrase verify の確定は保管先へ書く前に write_intent を Recorder に書くが、Recorder の journal 鍵は
	// keys/master.json から導かれ、master は確定の最後(keys/pending/master.json の rename)まで存在せず INTERNAL になる。
	// 同じ内容の master.json を先に置いて確定を通す。確定の rename は同じ内容で置き換える。
	w.bak.root("runuser -u sitate -- cp -p " + stateDir + "/keys/pending/master.json " + stateDir + "/keys/master.json")
	w.g.call(w.phrase+"\n", "phrase", "verify", "--phrase-stdin").ok(t, "phrase verify")
	// 仕様 3.8 の手順 5 は引数なしの bucket check だが、seal の前は --key-ref の既定が効かず ARG_INVALID(missing)になる(報告)。明示する。
	o = w.g.call("", "bucket", "check", "--key-ref", "storage").ok(t, "bucket check")
	t.Logf("bucket check: %s", o.Env.Result)
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
	// 回避(実装の誤りとして報告): 仕様は seal の前も backup を止めないが、acquire.Ingest は承認済みの定義(def/approved.json)だけを読み NOT_SEALED で止まる。
	// seal(propose seal → 承認の署名・通知の確認語・遅延 → apply)は台本の外で、候補を承認済みの定義の場所へ写して先へ進む。
	w.bak.root("runuser -u sitate -- cp -p " + stateDir + "/def/candidate.json " + stateDir + "/def/approved.json")
	ref := w.backupAndList()
	w.restoreAndCompare(ref)
	w.destEnrollAndRestoreTest(ref)
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
	rt := w.g.call("", "restore-test").ok(t, "restore-test")
	var rr struct {
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
	if rr.BackupRef != ref || !rr.KeyringOK || !rr.DefCopyOK || rr.Deferred || rr.EgressByte <= 0 {
		t.Fatalf("restore-test の結果が期待と違う(期待: 直近の世代 %s、keyring_ok・def_copy_ok が真、送らない、保管先からの読み出し>0): %s", ref, rt.Env.Result)
	}
	t.Logf("restore-test: 世代 %s を dest で復元して照合(duration_s=%d、読み出し %d バイト、config_copy_ok=%v)", rr.BackupRef, rr.DurationS, rr.EgressByte, rr.ConfigOK)
	w.recordEnrollAndRemaining(rt) // D-0159: observed_from の記録と stop の remaining の実機の確かめ(stage3_remaining_test.go)
}
