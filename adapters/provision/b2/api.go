package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const (
	defaultAuthURL = "https://api.backblazeb2.com/b2api/v4/b2_authorize_account"
	apiPath        = "/b2api/v4/"
	apiBodyMax     = 16 << 20
)

// apiError は事業者の API の失敗。netReason が空でなければ応答が無い失敗(connect / timeout)。
type apiError struct {
	status     int
	code       string
	message    string
	netReason  string
	retryAfter time.Duration
	auth       bool
}

func (e *apiError) Error() string { return "b2 api error" }

func (e *apiError) retryable() bool { return e.netReason != "" || e.status == 429 || e.status >= 500 }

// client は B2 の API を呼ぶ。プロキシは使わない(spec 3.2.1 の「環境」)。再試行は接続の失敗・応答の時限切れ・429 と 5xx だけ。
type client struct {
	http    *http.Client
	authURL string
	sleep   func(context.Context, time.Duration)
	jitter  func(time.Duration) time.Duration
}

// newClient は認可の宛先を決める。testAuthURL は試験の切り替えの変数(SITATE_TEST_B2_AUTH_URL)の値で、空なら製品の宛先。
func newClient(testAuthURL string) *client {
	d := &net.Dialer{Timeout: contract.AdapterConnectTimeout}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           d.DialContext,
		TLSHandshakeTimeout:   contract.AdapterConnectTimeout,
		ResponseHeaderTimeout: contract.AdapterConnectTimeout * 2,
	}
	auth := defaultAuthURL
	if testAuthURL != "" {
		auth = testAuthURL
	}
	return &client{
		http:    &http.Client{Transport: tr},
		authURL: auth,
		sleep: func(ctx context.Context, d time.Duration) {
			select {
			case <-time.After(d):
			case <-ctx.Done():
			}
		},
		jitter: func(d time.Duration) time.Duration { return time.Duration(rand.Int63n(int64(d) + 1)) },
	}
}

// do は 1 回の要求を再試行つきで行い、成功した本文を out に読む。
func (c *client) do(ctx context.Context, build func() (*http.Request, error), out any, auth bool) error {
	var last *apiError
	for attempt := 0; ; attempt++ {
		req, err := build()
		if err != nil {
			return err
		}
		last = c.once(req.WithContext(ctx), out)
		if last == nil {
			return nil
		}
		last.auth = auth
		if !last.retryable() || attempt >= contract.AdapterRetryMax || ctx.Err() != nil {
			return last
		}
		c.sleep(ctx, c.backoff(attempt, last))
	}
}

// backoff は次の試みまでの間隔: 底 × 2^回数に 0〜計算値の揺らぎを足す。429・503 の Retry-After があればその秒数。
func (c *client) backoff(attempt int, e *apiError) time.Duration {
	if e.retryAfter > 0 && (e.status == 429 || e.status == 503) {
		return e.retryAfter
	}
	d := contract.AdapterBackoffBase << attempt
	return d + c.jitter(d)
}

func (c *client) once(req *http.Request, out any) *apiError {
	resp, err := c.http.Do(req)
	if err != nil {
		reason := "connect"
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			reason = "timeout"
		}
		return &apiError{netReason: reason}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, apiBodyMax))
	if err != nil {
		return &apiError{netReason: "connect"}
	}
	if resp.StatusCode == http.StatusOK {
		if out != nil && json.Unmarshal(body, out) != nil {
			return &apiError{status: resp.StatusCode, code: "bad_body"}
		}
		return nil
	}
	e := &apiError{status: resp.StatusCode}
	var eb struct{ Code, Message string }
	if json.Unmarshal(body, &eb) == nil {
		e.code, e.message = eb.Code, eb.Message
	}
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
		e.retryAfter = time.Duration(s) * time.Second
	}
	return e
}

// session は認可の結果(トークンは持つだけで書き出さない)。
type session struct {
	c         *client
	accountID string
	token     string
	apiURL    string
	s3URL     string
	caps      []string
	namePfx   string
	role      string
}

type authResp struct {
	AccountID string `json:"accountId"`
	Token     string `json:"authorizationToken"`
	APIInfo   struct {
		Storage struct {
			APIURL  string `json:"apiUrl"`
			S3URL   string `json:"s3ApiUrl"`
			Allowed struct {
				Capabilities []string `json:"capabilities"`
				NamePrefix   *string  `json:"namePrefix"`
			} `json:"allowed"`
		} `json:"storageApi"`
	} `json:"apiInfo"`
}

// authorize は鍵で b2_authorize_account を呼ぶ。role は拒否の報告に使う枠の名前(admin / write / read)。
// 応答の s3ApiUrl が要求の endpoint と違えば、そのアカウントのバケットはその接続先に無いため BUCKET_NOT_OWNED(absent)。
func (c *client) authorize(ctx context.Context, keyID, secret, endpoint, role string) (*session, *provErr) {
	var ar authResp
	build := func() (*http.Request, error) {
		r, err := http.NewRequest(http.MethodGet, c.authURL, nil)
		if err != nil {
			return nil, err
		}
		r.SetBasicAuth(keyID, secret)
		return r, nil
	}
	if err := c.do(ctx, build, &ar, true); err != nil {
		return nil, mapAPIError(err, role)
	}
	st := ar.APIInfo.Storage
	if ar.Token == "" || st.APIURL == "" {
		return nil, internalErr("output")
	}
	if trimSlash(st.S3URL) != trimSlash(endpoint) {
		return nil, bucketNotOwned("absent")
	}
	s := &session{c: c, accountID: ar.AccountID, token: ar.Token, apiURL: trimSlash(st.APIURL), s3URL: st.S3URL, caps: st.Allowed.Capabilities, role: role}
	if st.Allowed.NamePrefix != nil {
		s.namePfx = *st.Allowed.NamePrefix
	}
	return s, nil
}

// call は Native API を POST で呼ぶ。失敗は *provErr に写して返す。
func (s *session) call(ctx context.Context, name string, body, out any) *provErr {
	err := s.callRaw(ctx, name, body, out)
	if err == nil {
		return nil
	}
	return mapAPIError(err, s.role)
}

// callRaw は写す前の失敗(*apiError)を返す。呼び出し側が 400 の本文で分岐するとき用。
func (s *session) callRaw(ctx context.Context, name string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	build := func() (*http.Request, error) {
		r, err := http.NewRequest(http.MethodPost, s.apiURL+apiPath+name, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Authorization", s.token)
		r.Header.Set("Content-Type", "application/json")
		return r, nil
	}
	return s.c.do(ctx, build, out, false)
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
