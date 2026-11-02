package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// sendTimeout は 1 回の送信の全体の時限。試験が短くする。
var sendTimeout = contract.AdapterConnectTimeout

// deliver は 1 回だけ POST する(再試行しない。spec 3.2.5)。配送の失敗は delivered: false と reason で返す。
func deliver(req request, extraCAFile string) contract.NotifyResult {
	u, err := url.Parse(req.url)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.Hostname() == "" || u.User != nil {
		return fail("url_invalid", nil)
	}
	pool, err := rootPool(extraCAFile)
	if err != nil {
		return fail("tls", nil) // 追加の CA が読めないときも検証を緩めず、配送しない
	}
	tr := &http.Transport{
		Proxy:               nil, // プロキシを使わない(spec 3.2.1)
		DialContext:         (&net.Dialer{Timeout: contract.AdapterConnectTimeout}).DialContext,
		TLSClientConfig:     &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: contract.AdapterConnectTimeout,
		DisableCompression:  true, // Accept-Encoding を足さない
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{
		Transport:     tr,
		Timeout:       sendTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, // たどらない
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(req.body))
	if err != nil {
		return fail("url_invalid", nil)
	}
	hr.Header.Set("Content-Type", "application/json; charset=utf-8")
	hr.Header.Set("User-Agent", "sitate-webhook/"+version)
	hr.Header.Set("Sitate-Event-Id", req.eventID)
	resp, err := cl.Do(hr)
	if err != nil {
		return fail(classify(err), nil)
	}
	defer resp.Body.Close()
	io.CopyN(io.Discard, resp.Body, contract.WebhookResponseMax) // 先頭の上限までを読んで捨てる
	st := resp.StatusCode
	switch {
	case st >= 200 && st < 300:
		return contract.NotifyResult{Contract: contract.AdapterContract["notify"], Delivered: true, Channel: "webhook", HTTPStatus: &st}
	case st >= 300 && st < 400:
		return fail("redirect", &st)
	}
	return fail("http_status", &st)
}

// rootPool は OS の信頼ストアに、あれば追加の CA のファイルを足す。
func rootPool(extraCAFile string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if extraCAFile == "" {
		return pool, nil
	}
	pem, err := os.ReadFile(extraCAFile)
	if err != nil || !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("extra ca")
	}
	return pool, nil
}

// classify は送信の失敗を reason に分ける。エラーの文(URL を含みうる)は使わない。
func classify(err error) string {
	var (
		unk  x509.UnknownAuthorityError
		inv  x509.CertificateInvalidError
		host x509.HostnameError
		ver  *tls.CertificateVerificationError
		rec  tls.RecordHeaderError
		ne   net.Error
	)
	switch {
	case errors.As(err, &unk), errors.As(err, &inv), errors.As(err, &host), errors.As(err, &ver), errors.As(err, &rec):
		return "tls"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	}
	return "connect"
}

func fail(reason string, status *int) contract.NotifyResult {
	return contract.NotifyResult{Contract: contract.AdapterContract["notify"], Channel: "webhook", Reason: &reason, HTTPStatus: status}
}
