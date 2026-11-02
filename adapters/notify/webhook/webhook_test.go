package main

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const secretToken = "tok-SECRET-1234"

const eventJSON = `{"schema":"sitate.event/v1","id":"e-1","time":"2026-10-18T17:35:12Z","severity":"info","type":"notify.test","repo_id":"rp-1","facts":{"confirmation_word":"みどり"},"suppressed_count":0,"next":[]}`

type got struct {
	mu     sync.Mutex
	n      int
	method string
	header http.Header
	body   []byte
	path   string
}

// startTLS は自己署名の受け手を起こし、その証明書(追加の CA として渡す)のファイルを返す。
func startTLS(t *testing.T, h http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	f := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(f, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return srv, f
}

func recorder(g *got, status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		g.mu.Lock()
		g.n++
		g.method, g.header, g.body, g.path = r.Method, r.Header.Clone(), b, r.URL.Path
		g.mu.Unlock()
		w.WriteHeader(status)
		w.Write([]byte(`{"ok":true}`))
	}
}

func reqLine(url string) string {
	b, _ := json.Marshal(map[string]any{"contract": "sitate.notify/v1", "to": "chat", "event": json.RawMessage(eventJSON), "secrets": map[string]string{"url": url}})
	return string(b) + "\n"
}

// call は run を呼び、終了コード・結果・標準出力の生の文字列を返す。
func call(t *testing.T, line, caFile string) (int, contract.NotifyResult, string) {
	t.Helper()
	var out bytes.Buffer
	env := func(k string) string {
		if k == contract.EnvExtraCAFile {
			return caFile
		}
		return ""
	}
	code := run(strings.NewReader(line), &out, []string{"run"}, env)
	var res contract.NotifyResult
	if code == 0 {
		if err := json.Unmarshal(out.Bytes(), &res); err != nil || !strings.HasSuffix(out.String(), "\n") || strings.Count(out.String(), "\n") != 1 {
			t.Fatalf("結果が JSON 1 行でない: %q", out.String())
		}
	}
	return code, res, out.String()
}

func wantFail(t *testing.T, res contract.NotifyResult, reason string, status *int) {
	t.Helper()
	if res.Delivered || res.Reason == nil || *res.Reason != reason || res.Channel != "webhook" {
		t.Fatalf("結果 %+v reason=%v、期待 %s", res, res.Reason, reason)
	}
	if (status == nil) != (res.HTTPStatus == nil) || (status != nil && *status != *res.HTTPStatus) {
		t.Fatalf("http_status %v、期待 %v", res.HTTPStatus, status)
	}
}

func ip(n int) *int { return &n }

func TestDeliveredWithExtraCA(t *testing.T) {
	var g got
	srv, ca := startTLS(t, recorder(&g, 200))
	code, res, _ := call(t, reqLine(srv.URL+"/hooks/"+secretToken), ca)
	if code != 0 || !res.Delivered || res.Reason != nil || res.Channel != "webhook" || res.ProviderRef != nil || res.HTTPStatus == nil || *res.HTTPStatus != 200 {
		t.Fatalf("code=%d res=%+v", code, res)
	}
	if g.n != 1 || g.method != "POST" || g.path != "/hooks/"+secretToken {
		t.Fatalf("n=%d method=%s path=%s", g.n, g.method, g.path)
	}
	if string(g.body) != eventJSON {
		t.Fatalf("本文がイベントそのものでない: %s", g.body)
	}
}

func TestHeadersExactlyThree(t *testing.T) {
	var g got
	srv, ca := startTLS(t, recorder(&g, 204))
	_, res, _ := call(t, reqLine(srv.URL), ca)
	if !res.Delivered {
		t.Fatalf("%+v", res)
	}
	want := map[string]string{
		"Content-Type":    "application/json; charset=utf-8",
		"User-Agent":      "sitate-webhook/" + version,
		"Sitate-Event-Id": "e-1",
	}
	for k, v := range want {
		if g.header.Get(k) != v {
			t.Errorf("%s = %q、期待 %q", k, g.header.Get(k), v)
		}
	}
	// 送り手が付けるのは上の 3 つと、HTTP の仕組み上の Content-Length だけ(Host は Header に出ない)。
	for k := range g.header {
		if _, ok := want[k]; !ok && k != "Content-Length" {
			t.Errorf("足していない見出し %s がある", k)
		}
	}
}

func TestSelfSignedWithoutCA(t *testing.T) {
	var g got
	srv, _ := startTLS(t, recorder(&g, 200))
	code, res, _ := call(t, reqLine(srv.URL), "")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	wantFail(t, res, "tls", nil)
	if g.n != 0 {
		t.Fatalf("検証に失敗したのに本文が届いた")
	}
}

func TestBrokenCAFileFailsClosed(t *testing.T) {
	var g got
	srv, _ := startTLS(t, recorder(&g, 200))
	bad := filepath.Join(t.TempDir(), "bad.pem")
	os.WriteFile(bad, []byte("not a pem"), 0o600)
	for _, f := range []string{bad, filepath.Join(t.TempDir(), "none.pem")} {
		_, res, _ := call(t, reqLine(srv.URL), f)
		wantFail(t, res, "tls", nil)
	}
	if g.n != 0 {
		t.Fatal("届いてはならない")
	}
}

func TestHTTPStatus(t *testing.T) {
	for _, st := range []int{400, 404, 500, 503} {
		var g got
		srv, ca := startTLS(t, recorder(&g, st))
		_, res, _ := call(t, reqLine(srv.URL), ca)
		wantFail(t, res, "http_status", ip(st))
		if g.n != 1 {
			t.Fatalf("再試行してはならない: n=%d", g.n)
		}
	}
}

func TestRedirectNotFollowed(t *testing.T) {
	var target got
	tsrv, ca := startTLS(t, recorder(&target, 200))
	var first got
	srv, _ := startTLS(t, func(w http.ResponseWriter, r *http.Request) {
		first.mu.Lock()
		first.n++
		first.mu.Unlock()
		http.Redirect(w, r, tsrv.URL+"/x", http.StatusTemporaryRedirect)
	})
	// 2 つの受け手は別の自己署名の証明書。両方を信頼させるため、2 つ目の CA を連結する。
	ca2 := filepath.Join(t.TempDir(), "both.pem")
	a, _ := os.ReadFile(ca)
	b := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	os.WriteFile(ca2, append(a, b...), 0o600)
	_, res, _ := call(t, reqLine(srv.URL), ca2)
	wantFail(t, res, "redirect", ip(307))
	if first.n != 1 || target.n != 0 {
		t.Fatalf("リダイレクトをたどった: first=%d target=%d", first.n, target.n)
	}
}

func TestURLInvalid(t *testing.T) {
	for _, u := range []string{"", "http://example.invalid/x", "ftp://example.invalid/", "://bad", "https://", "https:///path", "example.invalid/x", "https://user:pw@example.invalid/x", "%zz"} {
		code, res, _ := call(t, reqLine(u), "")
		if code != 0 {
			t.Fatalf("%q: code=%d", u, code)
		}
		wantFail(t, res, "url_invalid", nil)
	}
}

func TestConnectFailure(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close() // 閉じたポートへ
	_, res, _ := call(t, reqLine("https://"+addr+"/x"), "")
	wantFail(t, res, "connect", nil)
	_, res, _ = call(t, reqLine("https://nonexistent.invalid/x"), "")
	wantFail(t, res, "connect", nil)
}

func TestTimeout(t *testing.T) {
	old := sendTimeout
	sendTimeout = 300 * time.Millisecond
	defer func() { sendTimeout = old }()
	release := make(chan struct{})
	srv, ca := startTLS(t, func(w http.ResponseWriter, r *http.Request) { <-release })
	defer close(release)
	_, res, _ := call(t, reqLine(srv.URL), ca)
	wantFail(t, res, "timeout", nil)
}

func TestNoProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("https_proxy", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	var g got
	srv, ca := startTLS(t, recorder(&g, 200))
	_, res, _ := call(t, reqLine(srv.URL), ca)
	if !res.Delivered {
		t.Fatalf("プロキシの環境変数に左右されてはならない: %+v", res)
	}
}

func TestResponseBodyCapped(t *testing.T) {
	srv, ca := startTLS(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), 4*contract.WebhookResponseMax))
	})
	code, res, out := call(t, reqLine(srv.URL), ca)
	if code != 0 || !res.Delivered || strings.Contains(out, "xxxx") {
		t.Fatalf("code=%d out=%.200s", code, out)
	}
}

func TestSecretURLNeverInOutput(t *testing.T) {
	var g got
	srv, ca := startTLS(t, recorder(&g, 200))
	urls := []string{srv.URL + "/hooks/" + secretToken, "https://127.0.0.1:1/" + secretToken, "http://x.invalid/" + secretToken, "https://" + secretToken + ".invalid/"}
	for _, u := range urls {
		for _, c := range []string{ca, ""} {
			_, _, out := call(t, reqLine(u), c)
			if strings.Contains(out, secretToken) {
				t.Fatalf("URL(秘密)が結果に現れた: %s", out)
			}
		}
	}
}

// 実行ファイルを起動して、標準出力・標準エラーのどちらにも URL(秘密)が出ないことを確かめる。
func TestSecretURLNotInStdoutStderrOfBinary(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "webhook")
	if b, err := exec.Command("go", "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, b)
	}
	var g got
	srv, ca := startTLS(t, recorder(&g, 200))
	for _, c := range []struct{ url, ca string }{{srv.URL + "/hooks/" + secretToken, ca}, {srv.URL + "/hooks/" + secretToken, ""}, {"https://127.0.0.1:1/" + secretToken, ""}, {"http://x.invalid/" + secretToken, ""}} {
		cmd := exec.Command(exe, "run")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C", contract.EnvExtraCAFile + "=" + c.ca}
		cmd.Stdin = strings.NewReader(reqLine(c.url))
		var so, se bytes.Buffer
		cmd.Stdout, cmd.Stderr = &so, &se
		if err := cmd.Run(); err != nil {
			t.Fatalf("exit: %v", err)
		}
		if strings.Contains(so.String()+se.String(), secretToken) || se.Len() != 0 {
			t.Fatalf("stdout=%q stderr=%q", so.String(), se.String())
		}
	}
}

func TestRequestErrors(t *testing.T) {
	bad := []string{
		"", "not json\n", `{"contract":"sitate.notify/v2"}` + "\n",
		`{"contract":"sitate.notify/v1","to":"c","event":` + eventJSON + `,"secrets":{}}` + "\n",
		`{"contract":"sitate.notify/v1","to":"c","event":{"type":"x"},"secrets":{"url":"https://a.invalid/"}}` + "\n",
		`{"contract":"sitate.notify/v1","to":"c","event":` + eventJSON + `,"secrets":{"url":"https://a.invalid/"},"extra":1}` + "\n",
	}
	for _, l := range bad {
		var out bytes.Buffer
		code := run(strings.NewReader(l), &out, []string{"run"}, func(string) string { return "" })
		if code != 1 || !strings.Contains(out.String(), `"code":"INTERNAL"`) {
			t.Errorf("%q: code=%d out=%s", l, code, out.String())
		}
	}
	var out bytes.Buffer
	if run(strings.NewReader(reqLine("https://a.invalid/")), &out, nil, func(string) string { return "" }) != 1 {
		t.Error("引数なしは終了 1")
	}
}

func TestNoInsecureEscapeHatch(t *testing.T) {
	for _, f := range []string{"main.go", "send.go"} {
		b, _ := os.ReadFile(f)
		for _, bad := range []string{"InsecureSkipVerify", "VerifyPeerCertificate", "VerifyConnection", "ProxyFromEnvironment", "Getenv(\"HTTPS_PROXY\")"} {
			if bytes.Contains(b, []byte(bad)) {
				t.Errorf("%s に %s がある(GB-36)", f, bad)
			}
		}
	}
}
