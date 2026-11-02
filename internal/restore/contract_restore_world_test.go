package restore

// restore-test の世界の準備: キーリング・repo.json・config/<def_hash> を保管先に置き、記録した版 ID を状態へ置く
// (spec 3.5 の表の 1・2、4.2 の版 ID の固定、4.5 の objects[]・last_phrase_verify)。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

const keyringID = "kr-0123456789abcdef0123456789abcdef"

// worldOpts は世界の崩し方。零値は健全。
type worldOpts struct {
	Arch          string // 復元先の `uname -m`(空なら x86_64)
	KeyringTamper bool   // 保管先のキーリングの最新版のバイト列を、最後に開けた版と違うものにする
	UnknownKeyID  bool   // 保持中のマニフェストが、キーリングの鍵 ID に無いマスター鍵を参照する
	NoConfig      bool   // config/<def_hash> を置かない
	ConfigTamper  bool   // config/<def_hash> の中身が def_hash と合わない
}

func shaOfBytes(b []byte) string { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }

// manifestBody は世代のマニフェストの本体(fake の Codec で包んだもの。最小の項目だけ: def_hash・key_id・definition)。
func manifestBody(ref, defHash, keyID string, def []byte) string {
	c, _ := json.Marshal(map[string]any{"format": "sitate.manifest/v1", "manifest_id": ref, "def_hash": defHash, "key_id": keyID, "definition": json.RawMessage(def),
		"targets": []map[string]any{{"id": "t1", "kind": "files", "status": "ok", "files": 3, "bytes": 30}}})
	b, _ := fake.Codec{}.Encode(contract.MagicManifest, strings.TrimPrefix(ref, "m-"), contract.Header{}, c, nil)
	return string(b)
}

// recordObj は書き込み鍵で key を置き、応答の版 ID などを state.objects[] へ記録する。
func recordObj(t *testing.T, e *env, key string, body []byte, class contract.RetentionClass) string {
	t.Helper()
	ws := &fake.ObjectStore{Core: e.Core, Bucket: "b", Role: fake.RoleWrite, Params: contract.FixedRetention{}}
	pr, err := ws.Put(ctx, key, strings.NewReader(string(body)), class)
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := json.Marshal(map[string]any{"key": key, "version_id": pr.VersionID, "size": pr.Size, "etag": pr.ETag, "retain_until": ts(pr.RetainUntil), "written_at": ts(e.Clk.T)})
	if err := contract.Validate("data/state.json#/$defs/object_rec", rec); err != nil {
		t.Fatalf("準備の objects[] の要素がスキーマに合わない: %v", err)
	}
	var arr []json.RawMessage
	_ = json.Unmarshal(e.St.Items["objects"], &arr)
	e.St.Items["objects"], _ = json.Marshal(append(arr, rec))
	return pr.VersionID
}

// keyringWorld は定義 def のハッシュ、キーリング、repo.json、config を置いた世界を作る。
func keyringWorld(t *testing.T, def string, o worldOpts) *env {
	t.Helper()
	norm := []byte(def)
	dh, err := contract.CanonicalHash(norm)
	if err != nil {
		t.Fatal(err)
	}
	keyID := "k1"
	if o.UnknownKeyID {
		keyID = "k9"
	}
	e := rtWorldBody(t, manifestBody(refA, dh, keyID, norm), manifestBody(refB, dh, "k1", norm))
	e.St.Def = norm
	e.St.Items["repo_id"] = json.RawMessage(strconv_q(repoID))
	repo, _ := json.Marshal(map[string]any{"format_version": 1, "repo_id": repoID, "keyring_id": keyringID})
	recordObj(t, e, "repo.json", repo, contract.RetentionDaily)
	keyring := []byte("KEYRING-BYTES-V2")
	vid := recordObj(t, e, "keyring/"+keyringID, keyring, contract.RetentionDaily)
	verified := keyring
	if o.KeyringTamper {
		verified = []byte("KEYRING-BYTES-V2-ORIGINAL") // 最後に phrase verify で開けた版のバイト列(保管先の今のものと違う)
	}
	lpv, _ := json.Marshal(map[string]any{"keyring_version": 2, "keyring_version_id": vid, "keyring_hash": shaOfBytes(verified), "key_ids": []string{"k1"}, "at": ts(e.Clk.T)})
	e.St.Items["last_phrase_verify"] = lpv
	if !o.NoConfig {
		norm2 := norm
		if o.ConfigTamper {
			norm2 = []byte(`{"version":1,"tampered":true}`)
		}
		cfg, _ := json.Marshal(map[string]any{"def_hash": dh, "normalized": json.RawMessage(norm2)})
		sealed, _ := fake.Codec{}.Encode(contract.MagicConfig, dh, contract.Header{}, cfg, nil)
		recordObj(t, e, "config/"+dh, sealed, contract.RetentionDaily)
	}
	return e
}
