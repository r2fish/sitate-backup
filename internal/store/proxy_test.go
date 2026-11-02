package store

// 実物の ObjectStore を偽の保管先のサーバーに通すときの道具: 要求を記録し、応答の Date を制御し、応答を差し替えて割り込める代理。

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/fake/s3server"
)

type reqRec struct {
	Method, Path string
	Header       http.Header
	Body         []byte
}

type proxy struct {
	inner http.Handler
	mu    sync.Mutex
	reqs  []reqRec
	// date は応答の Date に使う時刻(nil は実時刻)。
	date func() time.Time
	// hook は n 番目(0 から)の要求の前に呼ばれ、0 でなければその状態で応答して中身のサーバーへ渡さない。
	hook     func(n int, r *http.Request, w http.ResponseWriter) int
	inflight int
	maxIn    int
	delay    time.Duration
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	p.mu.Lock()
	n := len(p.reqs)
	p.reqs = append(p.reqs, reqRec{Method: r.Method, Path: r.URL.RequestURI(), Header: r.Header.Clone(), Body: body})
	p.inflight++
	p.maxIn = max(p.maxIn, p.inflight)
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.inflight--; p.mu.Unlock() }()
	if p.delay > 0 {
		time.Sleep(p.delay)
	}
	d := time.Now().UTC()
	if p.date != nil {
		d = p.date()
	}
	w.Header().Set("Date", d.Format(http.TimeFormat))
	if p.hook != nil {
		if st := p.hook(n, r, w); st != 0 {
			w.WriteHeader(st)
			return
		}
	}
	p.inner.ServeHTTP(&dateWriter{ResponseWriter: w, date: d}, r)
}

// dateWriter は中身のサーバーが Date を上書きしないようにする(net/http は未設定のときだけ入れる)。
type dateWriter struct {
	http.ResponseWriter
	date  time.Time
	wrote bool
}

func (d *dateWriter) WriteHeader(c int) {
	d.wrote = true
	d.Header().Set("Date", d.date.Format(http.TimeFormat))
	d.ResponseWriter.WriteHeader(c)
}

func (d *dateWriter) Write(b []byte) (int, error) {
	if !d.wrote {
		d.WriteHeader(http.StatusOK)
	}
	return d.ResponseWriter.Write(b)
}

// puts は PutObject の要求(保持の設定などの副リソースの要求を除く)。
func (p *proxy) puts() []reqRec {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []reqRec
	for _, r := range p.reqs {
		if r.Method == "PUT" && strings.Contains(r.Path, "x-id=PutObject") {
			out = append(out, r)
		}
	}
	return out
}

func (p *proxy) count() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.reqs) }

// proxied は偽の保管先(Object Lock 有効のバケット b)の前に代理を置き、書き込み鍵でつながった ObjectStore を返す。
type proxied struct {
	S     *ObjectStore
	P     *proxy
	Core  *fake.Core
	Clock *fake.ManualClock
}

func newProxied(t *testing.T) *proxied {
	t.Helper()
	clk := &fake.ManualClock{T: time.Now().UTC().Truncate(time.Second)}
	core := fake.NewCore(clk)
	if err := core.CreateBucket("b", true); err != nil {
		t.Fatal(err)
	}
	srv := s3server.New(core, "ADMINKEY", "adminsecret")
	srv.Keys["WRITEKEY"] = s3server.Key{Secret: "writesecret", Role: "write"}
	p := &proxy{inner: srv, date: func() time.Time { return clk.T }}
	ts := httptest.NewServer(p)
	t.Cleanup(ts.Close)
	s := &ObjectStore{Endpoint: ts.URL, Bucket: "b", AccessKey: "WRITEKEY", SecretKey: "writesecret", Params: contract.FixedRetention{}}
	s.sleep = func(context.Context, time.Duration) error { return nil }
	s.Clock = simClock{clk}
	return &proxied{S: s, P: p, Core: core, Clock: clk}
}

// simClock はバックアップサーバーの時計の偽物: 保管先の模擬時間(応答の Date)に追従する。
type simClock struct{ c *fake.ManualClock }

func (s simClock) Now(context.Context) (time.Time, error) { return s.c.T, nil }

func setItem(sf *fake.StateFiles, item string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	if sf.Items == nil {
		sf.Items = map[string]json.RawMessage{}
	}
	sf.Items[item] = b
}
