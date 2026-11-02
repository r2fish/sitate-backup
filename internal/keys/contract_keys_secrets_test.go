package keys

// 秘密の扱いと keyring_version の契約試験(spec 3.1.4 の秘密の規則、3.2.6 の issue-keys、4.3 のキーリングの本体、3.1.2 の init・phrase verify)。
// 判定する内容: 発行した鍵の秘密は Secrets へ置かれ、使い終えたら Wipe される / 結果・封筒・警告・ログ・誤り(detail を含む)に出ない /
// 結果に出る鍵の項目は role・key_ref・key_id だけ / keyring_version は init の秘密のレコード・保留・phrase verify の結果で同じ値(1 から始まる)。
// 秘密の値はすべて合成の値。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// memSecrets は書かれた値を記録する偽の Secrets(未保存の参照名の Get は誤り)。FailPut が真なら Put が失敗する。
type memSecrets struct {
	mu      sync.Mutex
	Vals    map[string]json.RawMessage
	Puts    []string
	FailPut bool
}

func (m *memSecrets) Get(_ context.Context, ref string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.Vals[ref]; ok {
		return v, nil
	}
	return nil, errors.New("not found")
}

func (m *memSecrets) Put(_ context.Context, ref string, v json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailPut {
		return errors.New("put failed")
	}
	if m.Vals == nil {
		m.Vals = map[string]json.RawMessage{}
	}
	m.Vals[ref] = append(json.RawMessage(nil), v...)
	m.Puts = append(m.Puts, ref)
	return nil
}

func (m *memSecrets) Delete(context.Context, string) error { return nil }

// captureLogs は標準のログ(log と slog の既定)を捕まえ、戻す関数を返す。
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	oldSlog, oldOut, oldFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.SetOutput(&buf)
	t.Cleanup(func() { slog.SetDefault(oldSlog); log.SetOutput(oldOut); log.SetFlags(oldFlags) })
	return &buf
}

// errText は誤りの文字列・detail・next を連ねたもの(秘密が出ていないかを見る範囲)。
func errText(err error) string {
	s := err.Error()
	var ce *contract.Error
	if errors.As(err, &ce) {
		nb, _ := json.Marshal(ce.Next)
		s += " " + string(ce.Detail) + " " + string(nb) + " " + string(ce.Code)
	}
	return s
}

func TestContractKeysInitStorageSecrets(t *testing.T) {
	logs := captureLogs(t)
	e := initEnv(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`)
	sec := e.k.Secrets.(*memSecrets)
	r, err := e.k.InitStorage(ctx, initCall())
	harness.Shape(t, []string{"init-storage"}, r, err)
	// 秘密は Secrets へ置かれる(参照名は結果の key_ref。値は秘密ファイルの形 {"keys":[{role,key_id,secret}]}。seams.go)。
	var res struct {
		Keys []map[string]any `json:"keys"`
	}
	harness.Result(t, r, &res)
	if len(res.Keys) != 2 {
		t.Fatalf("keys[] は 2 つ(write, read): %s", r.Result)
	}
	ref, _ := res.Keys[0]["key_ref"].(string)
	if ref == "" || len(sec.Puts) == 0 {
		t.Fatalf("key_ref = %q, Secrets への Put = %v", ref, sec.Puts)
	}
	for i, role := range []string{"write", "read"} {
		k := res.Keys[i]
		if len(k) != 3 || k["role"] != role || k["key_ref"] != ref || k["key_id"] != "kid-"+role {
			t.Errorf("keys[%d] は role・key_ref・key_id の 3 項目だけ(role=%s, key_id=kid-%s, key_ref は共通): %v", i, role, role, k)
		}
	}
	var stored struct {
		Keys []struct {
			Role   string `json:"role"`
			KeyID  string `json:"key_id"`
			Secret string `json:"secret"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(sec.Vals[ref], &stored); err != nil || len(stored.Keys) != 2 {
		t.Fatalf("Secrets[%s] = %s (%v)", ref, sec.Vals[ref], err)
	}
	for i, role := range []string{"write", "read"} {
		if got := stored.Keys[i]; got.Role != role || got.KeyID != "kid-"+role || got.Secret != secretOf(role) {
			t.Errorf("Secrets に置いた鍵 %d = %+v (role・key_id・発行した秘密)", i, got)
		}
	}
	// 出力のどこにも秘密の値が出ない(結果・警告・封筒・フレーズ用の経路・ログ・鍵の ID を含むアダプタの呼び出し以外)。
	env := string(harness.Envelope([]string{"init-storage"}, r))
	wb, _ := json.Marshal(r.Warnings)
	for _, role := range []string{"write", "read"} {
		v := secretOf(role)
		for name, text := range map[string]string{"結果": string(r.Result), "封筒": env, "警告": string(wb), "ログ": logs.String()} {
			if strings.Contains(text, v) {
				t.Errorf("%s に %s の秘密が出た: %s", name, role, text)
			}
		}
	}
	if r.Phrase != nil {
		t.Errorf("init-storage はフレーズの経路を使わない")
	}
	// 使い終えた秘密は消す(呼び出し側が Wipe する)。
	for i, s := range e.ks.IssuedSecrets {
		if len(s.Reveal()) != 0 {
			t.Errorf("発行した秘密 %d を Wipe していない", i)
		}
	}
}

func TestContractKeysInitStorageSecretsNotInErrors(t *testing.T) {
	// 発行の途中の失敗・Secrets への書き込みの失敗で、手元にある秘密が誤り(文字列・detail・next)とログに出ない。
	all := []string{secretOf("write"), secretOf("read"), "ADMIN-SECRET-VALUE"}
	cases := map[string]func(e *kenv){
		"read の発行が失敗": func(e *kenv) { e.ks.FailRole = "read" },
		"Put が失敗":     func(e *kenv) { e.k.Secrets.(*memSecrets).FailPut = true },
	}
	for name, setup := range cases {
		logs := captureLogs(t)
		e := initEnv(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`)
		setup(e)
		r, err := e.k.InitStorage(ctx, initCall())
		harness.Implemented(t, err)
		if err == nil {
			t.Errorf("%s: 失敗を返さなかった", name)
			continue
		}
		text := errText(err) + " " + string(r.Result) + " " + logs.String()
		for _, v := range all {
			if strings.Contains(text, v) {
				t.Errorf("%s: 秘密 %q が誤り・結果・ログに出た: %s", name, v, text)
			}
		}
	}
}

func TestContractKeysPhraseVerifyKeyringVersion(t *testing.T) {
	// 結果の keyring_version と last_phrase_verify は、保留のキーリングの版そのもの(固定の 1 ではない)。
	e := newPhraseEnv(true)
	e.ks.Ver = 3
	r, err := e.k.PhraseVerify(ctx, phraseCall(phraseTestPhrase))
	harness.Shape(t, []string{"phrase", "verify"}, r, err)
	if v := jsonField(t, r.Result, "keyring_version"); v != float64(3) {
		t.Errorf("keyring_version = %v (保留の版 3)", v)
	}
	if v := jsonField(t, e.st.Items["last_phrase_verify"], "keyring_version"); v != float64(3) {
		t.Errorf("last_phrase_verify.keyring_version = %v (3)", v)
	}
}

func TestContractKeysInitPhraseVerifyKeyringVersion(t *testing.T) {
	// init が出す秘密のレコードの keyring_version = 保留の版 = 保留のキーリングを開いた版 = phrase verify の結果。init の直後は 1。
	e := newPhraseEnv(false)
	ks := newKS()
	e.k.KeyS, e.k.Codec = ks, &harness.RefCodec{}
	pub := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x07}, 32))
	r, err := e.k.Init(ctx, contract.Call{Words: []string{"init"}, SecretOut: true, Args: map[string][]string{"approval-pubkey": {pub}}})
	harness.Shape(t, []string{"init"}, r, err)
	if r.Phrase == nil {
		t.Fatal("init が秘密のレコードを返さない(Reply.Phrase)")
	}
	rec := r.Phrase.Reveal()
	if verr := contract.Validate("cli/secret-record.json", rec); verr != nil {
		t.Fatalf("秘密のレコードの形が不正: %v", verr)
	}
	var sr struct {
		Words          []string `json:"words"`
		KeyringVersion int      `json:"keyring_version"`
	}
	_ = json.Unmarshal(rec, &sr)
	if sr.KeyringVersion != 1 {
		t.Errorf("init の秘密のレコードの keyring_version = %d (1 から始まる)", sr.KeyringVersion)
	}
	// フレーズは結果・警告・封筒に出ない。
	env := string(harness.Envelope([]string{"init"}, r))
	if strings.Contains(env, sr.Words[0]+" "+sr.Words[1]) || strings.Contains(env, `"words"`) {
		t.Errorf("封筒にフレーズが出た: %s", env)
	}
	pk, err := os.ReadFile(filepath.Join(e.dir, contract.KeysPendingDir, contract.KeysPendingKeyring))
	if err != nil {
		t.Fatalf("init が keys/pending/keyring を置いていない: %v", err)
	}
	phrase := strings.Join(sr.Words, " ")
	if got, err := ks.OpenedKeyring(ctx, contract.NewSecret([]byte(phrase)), pk); err != nil || got.Version != sr.KeyringVersion {
		t.Errorf("保留のキーリングを開いた版 = %d %v (秘密のレコード %d)", got.Version, err, sr.KeyringVersion)
	}
	vr, err := e.k.PhraseVerify(ctx, phraseCall(phrase))
	harness.Shape(t, []string{"phrase", "verify"}, vr, err)
	if v := jsonField(t, vr.Result, "keyring_version"); v != float64(sr.KeyringVersion) {
		t.Errorf("phrase verify の keyring_version = %v (init の秘密のレコード %d と同じ)", v, sr.KeyringVersion)
	}
	if !slices.Contains([]string{"true"}, string(e.st.Items["phrase_confirmed"])) {
		t.Errorf("phrase_confirmed = %s", e.st.Items["phrase_confirmed"])
	}
}
