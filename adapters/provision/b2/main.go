// Command b2 は kind provision の同梱アダプタ `b2`(spec 3.2.6)。Backblaze B2 の Native API v4 で、バケットの作成・鍵の発行と失効・設定と鍵の検査を行う。
// 管理用鍵の秘密は要求の secrets の枠でだけ受け、標準エラーにもログにも出さない(結果 JSON の issue-keys の secret を除く)。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func main() {
	env := func(k string) string { return os.Getenv(k) }
	os.Exit(run(context.Background(), os.Stdin, os.Stdout, os.Stderr, os.Args[1:], env, newClient(env(contract.EnvTestB2AuthURL))))
}

// run は引数 `run` で要求を 1 行読み、結果を 1 行出す。終了コードを返す(0: 結果、1: error)。
func run(ctx context.Context, in io.Reader, out, errw io.Writer, args []string, _ func(string) string, c *client) int {
	if len(args) != 1 || args[0] != "run" {
		return emitErr(out, errw, "", internalErr("request"))
	}
	req, perr := readRequest(in)
	if perr != nil {
		return emitErr(out, errw, "", perr)
	}
	res, perr := dispatch(ctx, c, req)
	if perr != nil {
		return emitErr(out, errw, req.Op, perr)
	}
	return emitResult(out, errw, req.Op, res)
}

func dispatch(ctx context.Context, c *client, req *request) (map[string]any, *provErr) {
	switch req.Op {
	case "create-bucket":
		return createBucket(ctx, c, req)
	case "issue-keys":
		return issueKeys(ctx, c, req)
	case "revoke-keys":
		return revokeKeys(ctx, c, req)
	default:
		return checkOp(ctx, c, req)
	}
}

func emitResult(out, errw io.Writer, op string, res map[string]any) int {
	res["contract"] = contract.AdapterContract["provision"]
	b, err := json.Marshal(res)
	if err != nil || len(b) > contract.AdapterResultMax {
		return emitErr(out, errw, op, internalErr("output"))
	}
	fmt.Fprintf(out, "%s\n", b)
	return 0
}

// emitErr は終了 1 の error の 1 行を出す。標準エラーには操作とコードだけを書く(応答の本文・鍵の値は書かない)。
func emitErr(out, errw io.Writer, op string, e *provErr) int {
	body := map[string]any{"code": e.code, "detail": e.detail}
	if len(e.orphaned) > 0 {
		body["orphaned_key_ids"] = e.orphaned
	}
	b, _ := json.Marshal(map[string]any{"contract": contract.AdapterContract["provision"], "error": body})
	fmt.Fprintf(out, "%s\n", b)
	fmt.Fprintf(errw, "b2: op=%s error=%s\n", op, e.code)
	return 1
}
