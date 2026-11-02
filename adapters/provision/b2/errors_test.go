package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func checkAdminReq() map[string]any {
	return baseReq("check", map[string]any{"prefix": "sitate/", "key_role": nil})
}

func TestErrorMappingTable(t *testing.T) {
	cases := []struct {
		name   string
		api    string
		fail   fakeFail
		code   string
		detail string
	}{
		{"認可の 401 は管理用鍵の拒否", "b2_authorize_account", fakeFail{status: 401, code: "unauthorized", message: "x"}, "KEY_MISSING_PERMISSION", `"reason":"key_rejected"`},
		{"認可の 403 の上限", "b2_authorize_account", fakeFail{status: 403, code: "transaction_cap_exceeded", message: "x"}, "STORAGE_DENIED", `"reason":"provider_cap"`},
		{"アカウントの停止", "b2_list_buckets", fakeFail{status: 403, code: "access_denied", message: "account is suspended"}, "STORAGE_DENIED", `"reason":"account_suspended"`},
		{"支払い", "b2_list_buckets", fakeFail{status: 403, code: "access_denied", message: "payment required"}, "STORAGE_DENIED", `"reason":"payment"`},
		{"権限の欠け", "b2_list_buckets", fakeFail{status: 401, code: "unauthorized", message: "not allowed"}, "KEY_MISSING_PERMISSION", `"reason":"denied"`},
		{"想定外の 400", "b2_list_buckets", fakeFail{status: 400, code: "bad_request", message: "x"}, "adapter:b2:UNEXPECTED_RESPONSE", `"http_status":400`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := fakeWithBucket(t)
			for i := 0; i < 5; i++ {
				f.failNext(c.api, c.fail)
			}
			o := f.run(t, checkAdminReq())
			o.mustErr(t, c.code)
			if !strings.Contains(o.stdout, c.detail) {
				t.Errorf("%s を含むはず: %s", c.detail, o.stdout)
			}
			if f.count(c.api) != 1 {
				t.Errorf("再試行しない応答: %d 回", f.count(c.api))
			}
		})
	}
}

func TestRetryOnlyTransient(t *testing.T) {
	f := fakeWithBucket(t)
	f.failNext("b2_list_buckets", fakeFail{status: 500, code: "internal_error"}, fakeFail{status: 429, code: "too_many_requests", header: map[string]string{"Retry-After": "7"}})
	var slept []time.Duration
	c := f.client()
	c.sleep = func(_ context.Context, d time.Duration) { slept = append(slept, d) }
	o := f.runWith(t, c, checkAdminReq())
	o.mustOK(t)
	if f.count("b2_list_buckets") != 3 || len(slept) != 2 || slept[0] != contract.AdapterBackoffBase || slept[1] != 7*time.Second {
		t.Errorf("1 秒の底の指数の間隔と Retry-After: calls=%d slept=%v", f.count("b2_list_buckets"), slept)
	}
}

func TestRetryGivesUpAfterMax(t *testing.T) {
	for _, c := range []struct {
		status int
		reason string
	}{{503, "server_error"}, {429, "throttled"}} {
		f := fakeWithBucket(t)
		for i := 0; i < 10; i++ {
			f.failNext("b2_list_buckets", fakeFail{status: c.status, code: "busy"})
		}
		o := f.run(t, checkAdminReq())
		o.mustErr(t, "STORAGE_UNREACHABLE")
		d := o.err["detail"].(map[string]any)
		if d["reason"] != c.reason || d["target"] != "storage" || d["http_status"] != float64(c.status) {
			t.Errorf("%s", o.stdout)
		}
		if got := f.count("b2_list_buckets"); got != 1+contract.AdapterRetryMax {
			t.Errorf("再試行は %d 回まで: 呼び %d", contract.AdapterRetryMax, got)
		}
	}
}

func TestConnectFailure(t *testing.T) {
	f := fakeWithBucket(t)
	c := f.client()
	f.srv.Close()
	n := 0
	c.sleep = func(context.Context, time.Duration) { n++ }
	o := f.runWith(t, c, checkAdminReq())
	o.mustErr(t, "STORAGE_UNREACHABLE")
	d := o.err["detail"].(map[string]any)
	if d["reason"] != "connect" || d["http_status"] != nil || n != contract.AdapterRetryMax {
		t.Errorf("%s retries=%d", o.stdout, n)
	}
}

func TestNoSecretsInAnyErrorOutput(t *testing.T) {
	f := fakeWithBucket(t)
	f.failNext("b2_list_buckets", fakeFail{status: 400, code: "bad_request", message: "echo " + adminSecret + " " + testToken})
	o := f.run(t, checkAdminReq())
	for _, s := range []string{adminSecret, testToken} {
		if strings.Contains(o.stdout, s) || strings.Contains(o.stderr, s) {
			t.Errorf("秘密が出力に現れた: stdout=%q stderr=%q", o.stdout, o.stderr)
		}
	}
	if o.stderr != "b2: op=check error=adapter:b2:UNEXPECTED_RESPONSE\n" {
		t.Errorf("標準エラーは操作とコードだけ: %q", o.stderr)
	}
}

func TestAuthURLSwitch(t *testing.T) {
	if newClient("").authURL != defaultAuthURL || newClient("http://127.0.0.1:1/x").authURL != "http://127.0.0.1:1/x" {
		t.Error("試験の変数があれば宛先を替え、無ければ製品の宛先")
	}
	if !strings.HasPrefix(defaultAuthURL, "https://api.backblazeb2.com/b2api/v4/") {
		t.Error("製品の宛先")
	}
}
