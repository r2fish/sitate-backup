// Command webhook は kind notify の同梱アダプタ `webhook`(spec 3.2.5)。
// 要求の event を HTTPS の POST でそのまま送り、結果 JSON を 1 行出す。URL は秘密なので、出力・ログのどこにも書かない。
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"unicode/utf8"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// version は User-Agent に載せる(`sitate-webhook/<version>`)。adapter.json の version と合わせる。
const version = "0.0.0"

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Args[1:], os.Getenv))
}

// run は引数 `run` で要求を 1 行読み、結果を出す。終了コードを返す(0: 結果、1: error)。標準エラーには何も書かない。
func run(in io.Reader, out io.Writer, args []string, getenv func(string) string) int {
	if len(args) != 1 || args[0] != "run" {
		return failJSON(out, "request")
	}
	line, err := bufio.NewReaderSize(in, 1<<16).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return failJSON(out, "request")
	}
	req, reason := parseRequest(line)
	if reason != "" {
		return failJSON(out, reason)
	}
	res := deliver(req, getenv(contract.EnvExtraCAFile))
	b, _ := json.Marshal(res)
	out.Write(append(b, '\n'))
	return 0
}

// request は検査済みの要求。
type request struct {
	url     string
	eventID string
	body    []byte
}

// parseRequest は要求を検査する。契約の版が違えば "contract"、ほかの不正は "request"。
func parseRequest(line []byte) (request, string) {
	var probe struct {
		Contract string `json:"contract"`
	}
	if json.Unmarshal(line, &probe) != nil {
		return request{}, "request"
	}
	if probe.Contract != contract.AdapterContract["notify"] {
		return request{}, "contract"
	}
	var req contract.NotifyRequest
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil {
		return request{}, "request"
	}
	u, ok := req.Secrets["url"]
	var ev struct {
		ID string `json:"id"`
	}
	if !ok || json.Unmarshal(req.Event, &ev) != nil || ev.ID == "" || !utf8.Valid(req.Event) || !bytes.HasPrefix(bytes.TrimSpace(req.Event), []byte("{")) {
		return request{}, "request"
	}
	return request{url: u, eventID: ev.ID, body: req.Event}, ""
}

// failJSON は終了 1 の error(INTERNAL)を出す。detail に URL などの値は入れない。
func failJSON(out io.Writer, reason string) int {
	b, _ := json.Marshal(map[string]any{
		"contract": contract.AdapterContract["notify"],
		"error":    map[string]any{"code": "INTERNAL", "detail": map[string]any{"component": "adapter", "reason": reason}},
	})
	out.Write(append(b, '\n'))
	return 1
}
