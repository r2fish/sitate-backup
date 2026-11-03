package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const (
	testEndpoint  = "https://s3.us-west-004.backblazeb2.com"
	adminKeyID    = "004adminkeyid0001"
	adminSecret   = "K004-admin-secret-value-0123456789"
	testToken     = "tok-0123456789-authorization-token"
	testAccountID = "acc0001"
)

// fakeFail は API ごとの失敗の注入。status 0 は素通し。
type fakeFail struct {
	status  int
	code    string
	message string
	header  map[string]string
}

type fakeCred struct {
	secret    string
	caps      []string
	bucketIDs []string
	namePfx   string
	admin     bool
}

type fakeKey struct {
	listedKey
	secret string
}

// fakeB2 は B2 Native API v4 の偽のサーバー(試験用)。公開の文書の形に倣う。応答の形は実物で確かめていない。
type fakeB2 struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	creds   map[string]*fakeCred
	buckets map[string]*bucketInfo // 自分のアカウント
	others  map[string]bool        // 他のアカウントが持つ名前
	keys    []fakeKey
	s3URL   string
	page    int
	nextKey int
	inject  map[string][]fakeFail
	calls   []string
	bodies  map[string][]map[string]any
	authN   map[string]int // 鍵 ID ごとの認可の回数
	tokens  []string
}

func newFake(t *testing.T) *fakeB2 {
	f := &fakeB2{t: t, creds: map[string]*fakeCred{}, buckets: map[string]*bucketInfo{}, others: map[string]bool{}, s3URL: testEndpoint,
		inject: map[string][]fakeFail{}, bodies: map[string][]map[string]any{}, authN: map[string]int{}}
	f.creds[adminKeyID] = &fakeCred{secret: adminSecret, admin: true, caps: []string{"listKeys", "writeKeys", "deleteKeys", "listBuckets", "writeBuckets", "readBuckets", "readBucketRetentions", "writeBucketRetentions"}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeB2) authURL() string { return f.srv.URL + "/b2api/v4/b2_authorize_account" }

func (f *fakeB2) failNext(name string, fails ...fakeFail) {
	f.inject[name] = append(f.inject[name], fails...)
}

func (f *fakeB2) count(name string) int {
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

func (f *fakeB2) writeErr(w http.ResponseWriter, fl fakeFail) {
	for k, v := range fl.header {
		w.Header().Set(k, v)
	}
	w.WriteHeader(fl.status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": fl.status, "code": fl.code, "message": fl.message})
}

func (f *fakeB2) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	f.calls = append(f.calls, name)
	if q := f.inject[name]; len(q) > 0 {
		fl := q[0]
		f.inject[name] = q[1:]
		if fl.status != 0 {
			f.writeErr(w, fl)
			return
		}
	}
	if name == "b2_authorize_account" {
		f.authorize(w, r)
		return
	}
	if r.Header.Get("Authorization") != testToken {
		f.writeErr(w, fakeFail{status: 401, code: "bad_auth_token", message: "bad token"})
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.bodies[name] = append(f.bodies[name], body)
	f.api(w, name, body)
}

func (f *fakeB2) authorize(w http.ResponseWriter, r *http.Request) {
	id, sec, ok := r.BasicAuth()
	c := f.creds[id]
	f.authN[id]++
	if !ok || c == nil || c.secret != sec {
		f.writeErr(w, fakeFail{status: 401, code: "unauthorized", message: "bad key"})
		return
	}
	allowed := map[string]any{"capabilities": c.caps, "buckets": []any{}, "namePrefix": nil}
	if c.namePfx != "" {
		allowed["namePrefix"] = c.namePfx
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"accountId": testAccountID, "authorizationToken": testToken,
		"apiInfo": map[string]any{"storageApi": map[string]any{"apiUrl": f.srv.URL, "s3ApiUrl": f.s3URL, "allowed": allowed, "infoType": "storageApi"}}})
}

func (f *fakeB2) api(w http.ResponseWriter, name string, body map[string]any) {
	enc := json.NewEncoder(w)
	switch name {
	case "b2_create_bucket":
		f.createBucket(w, enc, body)
	case "b2_list_buckets":
		out := []*bucketInfo{}
		if b := f.buckets[body["bucketName"].(string)]; b != nil {
			out = append(out, b)
		}
		_ = enc.Encode(map[string]any{"buckets": out})
	case "b2_update_bucket":
		f.updateBucket(w, enc, body)
	case "b2_create_key":
		f.nextKey++
		id := fmt.Sprintf("004key%04d", f.nextKey)
		k := fakeKey{secret: fmt.Sprintf("K004secret%04d-abcdefghij", f.nextKey)}
		k.Name, k.ID = body["keyName"].(string), id
		for _, c := range body["capabilities"].([]any) {
			k.Caps = append(k.Caps, c.(string))
		}
		for _, b := range body["bucketIds"].([]any) {
			k.BucketIDs = append(k.BucketIDs, b.(string))
		}
		np := body["namePrefix"].(string)
		k.NamePrefix = &np
		f.keys = append(f.keys, k)
		f.creds[id] = &fakeCred{secret: k.secret, caps: k.Caps, bucketIDs: k.BucketIDs, namePfx: np}
		_ = enc.Encode(map[string]any{"applicationKeyId": id, "applicationKey": k.secret})
	case "b2_delete_key":
		id := body["applicationKeyId"].(string)
		i := slices.IndexFunc(f.keys, func(k fakeKey) bool { return k.ID == id })
		if i < 0 {
			f.writeErr(w, fakeFail{status: 400, code: "bad_request", message: "applicationKeyId " + id + " does not exist"})
			return
		}
		f.keys = slices.Delete(f.keys, i, i+1)
		delete(f.creds, id)
		_ = enc.Encode(map[string]any{})
	case "b2_list_keys":
		f.listKeys(w, body)
	default:
		f.writeErr(w, fakeFail{status: 404, code: "not_found", message: name})
	}
}

func (f *fakeB2) listKeys(w http.ResponseWriter, body map[string]any) {
	start, _ := body["startApplicationKeyId"].(string)
	from := 0
	if start != "" {
		from = slices.IndexFunc(f.keys, func(k fakeKey) bool { return k.ID == start })
	}
	to := len(f.keys)
	var next any
	if f.page > 0 && from+f.page < to {
		to = from + f.page
		next = f.keys[to].ID
	}
	out := []listedKey{}
	for _, k := range f.keys[from:to] {
		out = append(out, k.listedKey)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": out, "nextApplicationKeyId": next})
}

func (f *fakeB2) createBucket(w http.ResponseWriter, enc *json.Encoder, body map[string]any) {
	bn := body["bucketName"].(string)
	if f.buckets[bn] != nil || f.others[bn] {
		f.writeErr(w, fakeFail{status: 400, code: "duplicate_bucket_name", message: "Bucket name is already in use."})
		return
	}
	if !f.onlyFields(w, body, "accountId", "bucketName", "bucketType", "fileLockEnabled") {
		return
	}
	f.buckets[bn] = lockedBucket(bn, body["fileLockEnabled"] == true, "", 0, "") // 実 API: 作成では既定保持を付けられない
	_ = enc.Encode(map[string]any{"bucketId": f.buckets[bn].ID})
}

func (f *fakeB2) updateBucket(w http.ResponseWriter, enc *json.Encoder, body map[string]any) {
	if !f.onlyFields(w, body, "accountId", "bucketId", "defaultRetention") {
		return
	}
	dr := body["defaultRetention"].(map[string]any)
	for _, b := range f.buckets {
		if b.ID == body["bucketId"] {
			m := dr["mode"].(string)
			per := dr["period"].(map[string]any)
			b.Lock.Value.Retention = &retention{Mode: &m, Period: &struct {
				Duration int64  `json:"duration"`
				Unit     string `json:"unit"`
			}{int64(per["duration"].(float64)), per["unit"].(string)}}
		}
	}
	_ = enc.Encode(map[string]any{})
}

// onlyFields は実 API と同じく未知の項目を 400 bad_request で拒む。
func (f *fakeB2) onlyFields(w http.ResponseWriter, body map[string]any, allowed ...string) bool {
	for k := range body {
		if !slices.Contains(allowed, k) {
			f.writeErr(w, fakeFail{status: 400, code: "bad_request", message: "unknown field in request: " + k})
			return false
		}
	}
	return true
}

func lockedBucket(name string, enabled bool, mode string, dur float64, unit string) *bucketInfo {
	b := &bucketInfo{ID: "bid-" + name, Name: name}
	b.Lock.Authorized = true
	b.Lock.Value = &struct {
		Enabled   bool       `json:"isFileLockEnabled"`
		Retention *retention `json:"defaultRetention"`
	}{Enabled: enabled}
	if mode != "" {
		b.Lock.Value.Retention = &retention{Mode: &mode, Period: &struct {
			Duration int64  `json:"duration"`
			Unit     string `json:"unit"`
		}{int64(dur), unit}}
	}
	return b
}

// addBucket は自分のアカウントのバケットを足す。
func (f *fakeB2) addBucket(b *bucketInfo) { f.buckets[b.Name] = b }

// addKey は一覧に鍵を足す(製品が発行したもの以外を作る試験用)。
func (f *fakeB2) addKey(id, name, bucketID, prefix string, caps ...string) {
	k := fakeKey{secret: "s-" + id}
	k.ID, k.Name, k.Caps = id, name, caps
	if bucketID != "" {
		k.BucketIDs = []string{bucketID}
	}
	if prefix != "<none>" {
		k.NamePrefix = &prefix
	}
	f.keys = append(f.keys, k)
}

// out は 1 回の run の結果。
type out struct {
	code   int
	stdout string
	stderr string
	res    map[string]any
	err    map[string]any
}

func (f *fakeB2) client() *client {
	c := newClient(f.authURL())
	c.sleep = func(context.Context, time.Duration) {}
	c.jitter = func(time.Duration) time.Duration { return 0 }
	return c
}

func (f *fakeB2) run(t *testing.T, req map[string]any) out {
	return f.runWith(t, f.client(), req)
}

func (f *fakeB2) runWith(t *testing.T, c *client, req map[string]any) out {
	t.Helper()
	b, _ := json.Marshal(req)
	var so, se bytes.Buffer
	code := run(context.Background(), bytes.NewReader(append(b, '\n')), &so, &se, []string{"run"}, nil, c)
	o := out{code: code, stdout: so.String(), stderr: se.String()}
	if strings.Count(o.stdout, "\n") != 1 || !strings.HasSuffix(o.stdout, "\n") {
		t.Fatalf("標準出力は JSON 1 行: %q", o.stdout)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(o.stdout), &m); err != nil {
		t.Fatal(err)
	}
	if e, ok := m["error"].(map[string]any); ok {
		o.err = e
	} else {
		o.res = m
	}
	return o
}

func (o out) mustOK(t *testing.T) {
	t.Helper()
	if o.code != 0 || o.res == nil {
		t.Fatalf("成功のはず: code=%d %s", o.code, o.stdout)
	}
	o.validate(t)
}

func (o out) mustErr(t *testing.T, code string) {
	t.Helper()
	if o.code != 1 || o.err == nil || o.err["code"] != code {
		t.Fatalf("error.code=%s のはず: code=%d %s", code, o.code, o.stdout)
	}
	if code != "KEY_MISSING_PERMISSION" { // 契約のスキーマの error.code の列挙に無い(報告に記載)
		o.validate(t)
	}
}

func (o out) validate(t *testing.T) {
	t.Helper()
	if err := contract.Validate("adapter/provision.json#/$defs/response", []byte(o.stdout)); err != nil {
		t.Errorf("応答のスキーマに合わない: %v\n%s", err, o.stdout)
	}
}

func baseReq(op string, extra map[string]any) map[string]any {
	m := map[string]any{"contract": "sitate.provision/v1", "op": op, "endpoint": testEndpoint, "bucket": "b1",
		"secrets": map[string]any{"admin": map[string]any{"key_id": adminKeyID, "secret": adminSecret}}}
	for k, v := range extra {
		m[k] = v
	}
	return m
}
