package keys

// init-storage の鍵の参照名・管理用鍵・全部か無しか(spec 3.1.2 `init-storage`、3.2.6 `issue-keys`、4.5「参照名と秘密ファイル」)。
// 参照名は定義の storage[0].key_ref で、定数を埋めた実装は落ちる。issue-keys は 1 回の呼び出しで roles の組を発行する。

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

const candOtherRef = `{"version":1,"storage":[{"endpoint":"https://s3.example.invalid","bucket":"bkt-1","prefix":"sitate/","key_ref":"offsite-main"}]}`

func putRefs(e *kenv) []string {
	var refs []string
	for _, p := range e.k.Secrets.(*memSecrets).Puts {
		refs = append(refs, p)
	}
	return refs
}

// 鍵の参照名は候補の定義の storage[0].key_ref。結果の key_ref も Secrets への Put も同じ名前で、値は kind: storage_keys の秘密ファイルの形。
func TestContractKeysInitStorageKeyRefFromDefinition(t *testing.T) {
	for _, tc := range []struct{ def, ref string }{{candStorage, "store-main"}, {candOtherRef, "offsite-main"}} {
		e := initEnv(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`)
		e.st.candDef = tc.def
		r, err := e.k.InitStorage(ctx, initCall())
		harness.Shape(t, []string{"init-storage"}, r, err)
		if err != nil {
			continue
		}
		var res struct {
			Keys []struct {
				KeyRef string `json:"key_ref"`
			} `json:"keys"`
		}
		harness.Result(t, r, &res)
		if len(res.Keys) != 2 || res.Keys[0].KeyRef != tc.ref || res.Keys[1].KeyRef != tc.ref {
			t.Errorf("結果の key_ref は定義の %s: %s", tc.ref, r.Result)
		}
		if refs := putRefs(e); !slices.Equal(refs, []string{tc.ref}) {
			t.Errorf("Secrets.Put の参照名は定義の %s: %v", tc.ref, refs)
		}
		raw, _ := e.k.Secrets.Get(ctx, tc.ref)
		var sk contract.StorageKeysSecret
		if json.Unmarshal(raw, &sk) != nil || sk.Kind != contract.SecretKindStorageKeys || len(sk.Keys) != 2 || sk.Keys[0].Role != "write" || sk.Keys[1].Role != "read" {
			t.Errorf("秘密ファイルが kind: storage_keys の {keys:[write, read]} でない: %s", raw)
		}
	}
}

// 候補の定義が無い(def validate の前)なら止まり、バケット作成も発行も鍵の保存もしない。
func TestContractKeysInitStorageNoCandidate(t *testing.T) {
	e := initEnv(`{}`)
	e.st.cand = false
	_, err := e.k.InitStorage(ctx, initCall())
	harness.Implemented(t, err)
	if err == nil {
		t.Fatal("候補の定義が無いのに成功した")
	}
	if n := e.run.called("create-bucket") + len(e.ks.Reqs) + len(putRefs(e)); n != 0 {
		t.Errorf("候補が無いのに作成・発行・保存を行った: %d", n)
	}
}

// 発行は 1 回の呼び出し(roles: write, read)で、事業者の管理用鍵・保管先・接頭辞を渡す。
func TestContractKeysInitStorageIssueRequest(t *testing.T) {
	e := initEnv(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`)
	_, err := e.k.InitStorage(ctx, initCall())
	harness.Implemented(t, err)
	if len(e.ks.Reqs) != 1 {
		t.Fatalf("IssueKeys は 1 回: %d", len(e.ks.Reqs))
	}
	q := e.ks.Reqs[0]
	if !slices.Equal(q.Roles, []string{"write", "read"}) || q.Endpoint != "https://s3.example.invalid" || q.Bucket != "bkt-1" || q.Prefix != "sitate/" {
		t.Errorf("IssueRequest = %+v", q)
	}
	if q.Admin == nil || string(q.Admin.Reveal()) != `{"key_id":"ADMIN-ID","secret":"ADMIN-SECRET-VALUE"}` {
		t.Errorf("事業者の管理用鍵(--admin-ref の値)を IssueRequest に渡していない")
	}
}

// 発行の失敗で失効にも失敗した鍵 ID(orphaned)は台帳に orphaned: true で載せ、鍵は何も保存しない。
func TestContractKeysInitStorageOrphanedFromIssue(t *testing.T) {
	e := initEnv(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`)
	e.ks.FailRole, e.ks.Orphans = "read", []string{"K8"}
	_, err := e.k.InitStorage(ctx, initCall())
	harness.Implemented(t, err)
	if err == nil {
		t.Fatal("発行の失敗が成功になった")
	}
	found := false
	for _, l := range e.st.Ledger {
		if jsonField(t, l, "key_id") == "K8" && jsonField(t, l, "orphaned") == true {
			found = true
		}
	}
	if !found {
		t.Errorf("台帳に K8 が orphaned: true で載っていない: %s", e.st.Ledger)
	}
	if n := len(putRefs(e)); n != 0 {
		t.Errorf("全部か無し: 発行に失敗したのに鍵を保存した: %d", n)
	}
}
