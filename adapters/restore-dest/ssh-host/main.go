// Command ssh-host は kind restore-dest の同梱アダプタ(起動 / 停止の組。spec 3.2.4、3.2.7、3.5)。
// SSH で到達できる既存ホストを復元先にする: enroll(専用ユーザー・復元先の鍵・隔離の設定)、start(確認と使い捨て暗号化領域)、
// stop(平文の消去とその確かめ)、inspect(残存の検出と掃引)。電源は入れも切りもしない。
// 契約: `<exe> run` が標準入力の 1 行目の要求 JSON を読み、結果 JSON を標準出力に 1 行で返す。秘密は要求の secrets の枠だけで受け、
// 標準出力・標準エラーに書かない。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const (
	contractVersion = "sitate.restore-dest/v1"
	maxRequestBytes = 1 << 20
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "run" {
		fmt.Fprintln(os.Stderr, "usage: ssh-host run")
		os.Exit(64)
	}
	os.Exit(run(os.Stdin, os.Stdout))
}

// run は要求を 1 行読んで処理し、結果の行を out に書く。終了コード(0: 結果、1: error)を返す。
func run(in io.Reader, out io.Writer) int {
	line, err := bufio.NewReaderSize(io.LimitReader(in, maxRequestBytes), 64<<10).ReadBytes('\n')
	if err != nil && err != io.EOF {
		return emitErr(out, internalErr("request"))
	}
	var req contract.RestoreDestRequest
	if json.Unmarshal(line, &req) != nil {
		return emitErr(out, internalErr("request"))
	}
	if req.Contract != contractVersion {
		return emitErr(out, internalErr("contract"))
	}
	res, aerr := dispatch(&req)
	if aerr != nil {
		return emitErr(out, aerr)
	}
	return emitJSON(out, res)
}

func dispatch(req *contract.RestoreDestRequest) (map[string]any, *adapterErr) {
	switch req.Op {
	case "enroll":
		return opEnroll(req)
	case "start":
		return opStart(req)
	case "stop":
		return opStop(req)
	case "inspect":
		return opInspect(req)
	case "expose", "release":
		return nil, notImplemented(req.Op)
	}
	// create / destroy は作成 / 破棄の組の操作で、この組(start_stop)のアダプタには無い。
	return nil, internalErr("request")
}

func emitJSON(out io.Writer, v map[string]any) int {
	v["contract"] = contractVersion
	b, err := json.Marshal(v)
	if err != nil {
		return emitErr(out, internalErr("output"))
	}
	_, _ = out.Write(append(b, '\n'))
	return 0
}

func emitErr(out io.Writer, e *adapterErr) int {
	b, err := json.Marshal(map[string]any{"contract": contractVersion, "error": e.body()})
	if err != nil {
		b = []byte(`{"contract":"` + contractVersion + `","error":{"code":"INTERNAL","detail":{"component":"adapter","reason":"output"}}}`)
	}
	_, _ = out.Write(append(b, '\n'))
	return 1
}
