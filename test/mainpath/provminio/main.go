//go:build mainpath

// Command provminio は主経路の第 2 段だけが使う kind provision の試験用アダプタ(置き場の名前は b2)。
// 同梱の b2 は Backblaze B2 の API にしか届かず、受入試験の環境(MinIO)では bucket check の権限の読み取り(check を枠 key で呼ぶ)に使えない。
// そこで、受入試験の環境が起動時に作った MinIO の書き込み鍵・読み取り鍵の権限(ポリシー)を、契約 sitate.provision/v1 の check の結果として返す。
// 返す権限は test/env/bin/test-env.sh のポリシーと同じ(書き込み鍵: 書き込み・一覧・読み出し・保持の読み取りと延長・版指定の削除。読み取り鍵: 一覧と読み出し)。
// B2 の API は呼ばない。ほかの操作は NOT_IMPLEMENTED を返す。製品のビルドには入らない(tags mainpath、test/ の下)。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

const contractName = "sitate.provision/v1"

type req struct {
	Op      string  `json:"op"`
	KeyRole *string `json:"key_role"`
	Secrets map[string]struct {
		KeyID  string `json:"key_id"`
		Secret string `json:"secret"`
	} `json:"secrets"`
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

func main() {
	if len(os.Args) != 2 || os.Args[1] != "run" {
		fail("INTERNAL", map[string]any{"component": "adapter", "http_status": nil, "kind": "provision", "name": "b2", "reason": "request"})
	}
	line, _ := bufio.NewReaderSize(os.Stdin, 1<<20).ReadBytes('\n')
	var q req
	if json.Unmarshal(line, &q) != nil {
		fail("INTERNAL", map[string]any{"component": "adapter", "http_status": nil, "kind": "provision", "name": "b2", "reason": "request"})
	}
	if q.Op != "check" {
		fail("NOT_IMPLEMENTED", map[string]any{"feature": q.Op})
	}
	k, ok := q.Secrets["key"]
	if !ok || q.KeyRole == nil || k.KeyID == "" { // adapter.json の secrets[] に key が無いと、核が枠を落とす
		fail("adapter:b2:KEY_SLOT_MISSING", map[string]any{})
	}
	out(0, map[string]any{"versioning": nil, "lock_mode": nil, "default_retention_days": nil,
		"keys": []map[string]any{{"role": *q.KeyRole, "key_id": k.KeyID, "prefix": nil, "capabilities": caps[*q.KeyRole]}}})
}
