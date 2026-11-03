//go:build mainpath

// Command provminio は主経路の第 2 段だけが使う kind provision の試験用アダプタ(置き場の名前は minio。init-storage・bucket check の --adapter minio で呼ぶ)。
// 同梱の b2 は Backblaze B2 の API にしか届かず、受入試験の環境(MinIO)では使えない。そこで、受入試験の環境が起動時に作った MinIO の
// バケット・書き込み鍵・読み取り鍵を、契約 sitate.provision/v1 の操作として返す(B2 の API は呼ばない)。
//   - create-bucket: バケットは環境が作ってある。設定(バージョニング・COMPLIANCE・既定保持日数)をそのまま返す。
//   - issue-keys: 環境が作った書き込み鍵・読み取り鍵を、要求の roles の順に返す(アダプタの同じ場所の keys.json から読む)。
//   - revoke-keys: 何もしない(環境の鍵は失効させない)。
//   - check: 管理用鍵(枠 admin)は印付きの鍵なし。鍵(枠 key)は、環境のポリシーと同じ権限(test/env/bin/test-env.sh)を返す
//     (書き込み鍵: 書き込み・一覧・読み出し・保持の読み取りと延長・版指定の削除。読み取り鍵: 一覧と読み出し)。
//
// 受け取る枠は adapter.json の secrets[](admin・key)で、核は宣言に無い枠を落とす。製品のビルドには入らない(tags mainpath、test/ の下)。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const contractName = "sitate.provision/v1"

type req struct {
	Op      string   `json:"op"`
	Roles   []string `json:"roles"`
	KeyRole *string  `json:"key_role"`
	Secrets map[string]struct {
		KeyID  string `json:"key_id"`
		Secret string `json:"secret"`
	} `json:"secrets"`
}

// envKeys は環境が作った鍵(置き場の keys.json。試験の環境の合成の鍵だけ)。
type envKeys struct {
	DefaultRetentionDays int `json:"default_retention_days"`
	Keys                 map[string]struct {
		KeyID  string `json:"key_id"`
		Secret string `json:"secret"`
	} `json:"keys"`
}

var caps = map[string][]string{
	"write": {"put", "list_versions", "get_version", "get_retention", "get_bucket_config", "extend_retention", "delete_version"},
	"read":  {"list_versions", "get_version"},
}

func out(code int, v map[string]any) {
	v["contract"] = contractName
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
	os.Exit(code)
}

func fail(code string, detail map[string]any) {
	out(1, map[string]any{"error": map[string]any{"code": code, "detail": detail}})
}

func internal(reason string) {
	fail("INTERNAL", map[string]any{"component": "adapter", "http_status": nil, "kind": "provision", "name": "minio", "reason": reason})
}

func loadKeys() envKeys {
	exe, err := os.Executable()
	if err != nil {
		internal("request")
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "keys.json"))
	var k envKeys
	if err != nil || json.Unmarshal(b, &k) != nil {
		internal("request")
	}
	return k
}

func main() {
	if len(os.Args) != 2 || os.Args[1] != "run" {
		internal("request")
	}
	line, _ := bufio.NewReaderSize(os.Stdin, 1<<20).ReadBytes('\n')
	var q req
	if json.Unmarshal(line, &q) != nil {
		internal("request")
	}
	switch q.Op {
	case "create-bucket":
		if _, ok := q.Secrets["admin"]; !ok { // adapter.json の secrets[] に admin が無いと、核が枠を落とす
			fail("adapter:minio:ADMIN_SLOT_MISSING", map[string]any{})
		}
		k := loadKeys()
		out(0, map[string]any{"created": false, "versioning": true, "lock_mode": "COMPLIANCE", "default_retention_days": k.DefaultRetentionDays})
	case "issue-keys":
		if _, ok := q.Secrets["admin"]; !ok {
			fail("adapter:minio:ADMIN_SLOT_MISSING", map[string]any{})
		}
		k := loadKeys()
		var issued []map[string]string
		for _, r := range q.Roles {
			e, ok := k.Keys[r]
			if !ok {
				internal("request")
			}
			issued = append(issued, map[string]string{"role": r, "key_id": e.KeyID, "secret": e.Secret})
		}
		out(0, map[string]any{"issued": issued})
	case "revoke-keys":
		out(0, map[string]any{"revoked": []string{}, "not_found": []string{}})
	case "check":
		if _, ok := q.Secrets["admin"]; ok {
			k := loadKeys()
			out(0, map[string]any{"versioning": true, "lock_mode": "COMPLIANCE", "default_retention_days": k.DefaultRetentionDays, "keys": []any{}})
		}
		kk, ok := q.Secrets["key"]
		if !ok || q.KeyRole == nil || kk.KeyID == "" { // adapter.json の secrets[] に key が無いと、核が枠を落とす
			fail("adapter:minio:KEY_SLOT_MISSING", map[string]any{})
		}
		out(0, map[string]any{"versioning": nil, "lock_mode": nil, "default_retention_days": nil,
			"keys": []map[string]any{{"role": *q.KeyRole, "key_id": kk.KeyID, "prefix": nil, "capabilities": caps[*q.KeyRole]}}})
	default:
		fail("NOT_IMPLEMENTED", map[string]any{"feature": q.Op})
	}
}
