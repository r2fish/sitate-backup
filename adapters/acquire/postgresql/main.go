// Command postgresql は kind acquire の同梱アダプタ `postgresql`(spec 3.2.2)。
// 受け口(spec 3.3)の `dump-pg-globals`・`dump-pg` を SITATE_RECV 経由で項目ごとに 1 回ずつ呼び(globals が先)、
// その標準出力を `dump` フレーム(format: globals | custom)の本体としてストリーム sitate.stream/v1 へ流し、最後に結果 JSON を 1 行出す。
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/r2fish/sitate-backup/internal/contract"
)

var targetIDRe = regexp.MustCompile(`^t[1-9][0-9]{0,3}$`)

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Args[1:]))
}

// run は引数 `run` で要求を 1 行読み、結果を出す。終了コードを返す(0: 結果、1: error)。
func run(in io.Reader, out io.Writer, args []string) int {
	if len(args) != 1 || args[0] != "run" {
		return failJSON(out, "INTERNAL", map[string]any{"component": "adapter", "reason": "request"})
	}
	line, err := bufio.NewReaderSize(in, 1<<16).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return failJSON(out, "INTERNAL", map[string]any{"component": "adapter", "reason": "request"})
	}
	req, code, detail := parseRequest(line)
	if code != "" {
		return failJSON(out, code, detail)
	}
	recv := os.Getenv(contract.EnvRecv)
	if recv == "" {
		return failJSON(out, "INTERNAL", map[string]any{"component": "adapter", "reason": "request"})
	}
	return acquire(out, recv, req.Target.ID)
}

// parseRequest は要求を検査する。契約の版が違えば reason: contract、ほかの不正は reason: request。
func parseRequest(line []byte) (contract.AcquireRequest, string, map[string]any) {
	var req contract.AcquireRequest
	var probe struct {
		Contract string `json:"contract"`
	}
	if json.Unmarshal(line, &probe) != nil {
		return req, "INTERNAL", map[string]any{"component": "adapter", "reason": "request"}
	}
	if probe.Contract != contract.AdapterContract["acquire"] {
		return req, "INTERNAL", map[string]any{"component": "adapter", "reason": "contract"}
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || req.Target.Kind != "postgresql" || !targetIDRe.MatchString(req.Target.ID) {
		return req, "INTERNAL", map[string]any{"component": "adapter", "reason": "request"}
	}
	return req, "", nil
}

// failJSON は終了 1 の error の 1 行を出す(detail は 3.1.3 の形)。
func failJSON(out io.Writer, code string, detail map[string]any) int {
	b, _ := json.Marshal(map[string]any{"contract": contract.AdapterContract["acquire"], "error": map[string]any{"code": code, "detail": detail}})
	fmt.Fprintf(out, "%s\n", b)
	return 1
}
