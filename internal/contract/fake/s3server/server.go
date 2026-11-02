package s3server

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

// Server は偽の保管先の S3 互換サーバー。
type Server struct {
	Core *fake.Core
	Keys map[string]Key
}

// New は Server を作る。admin は管理用鍵。
func New(core *fake.Core, accessKey, secret string) *Server {
	return &Server{Core: core, Keys: map[string]Key{accessKey: {Secret: secret, Role: "admin"}}}
}

type errXML struct {
	XMLName xml.Name `xml:"Error"`
	Code    string
	Message string
}

func writeErr(w http.ResponseWriter, err error) {
	var e *fake.S3Error
	if !errors.As(err, &e) {
		e = &fake.S3Error{Code: "InternalError", Status: 500, Msg: err.Error()}
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(e.Status)
	_ = xml.NewEncoder(w).Encode(errXML{Code: e.Code, Message: e.Msg})
}

// ServeHTTP は http.Handler を満たす。応答の Date には実時刻を返す。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	ak, ok := verify(r, s.Keys)
	if !ok {
		writeErr(w, &fake.S3Error{Code: "SignatureDoesNotMatch", Status: 403, Msg: "署名 v4 が合わない"})
		return
	}
	if err := s.Core.Healthy(); err != nil {
		writeErr(w, err)
		return
	}
	role := fake.Role(s.Keys[ak].Role)
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	q := r.URL.Query()
	delete(q, "x-id") // SDK が付ける操作名。意味を持たない
	var err error
	switch {
	case bucket == "":
		err = &fake.S3Error{Code: "NotImplemented", Status: 501, Msg: "ListBuckets は範囲外"}
	case key == "":
		err = s.bucketOp(w, r, bucket, q, role)
	default:
		err = s.objectOp(w, r, bucket, key, q, role)
	}
	if err != nil {
		writeErr(w, err)
	}
}

func notImpl(r *http.Request) error {
	return &fake.S3Error{Code: "NotImplemented", Status: 501, Msg: r.Method + " " + r.URL.RequestURI() + " は範囲外"}
}

func (s *Server) bucketOp(w http.ResponseWriter, r *http.Request, b string, q map[string][]string, role fake.Role) error {
	has := func(k string) bool { _, ok := q[k]; return ok }
	admin := func() error {
		if role != fake.RoleAdmin {
			return &fake.S3Error{Code: "AccessDenied", Status: 403, Msg: "管理用鍵だけ"}
		}
		return nil
	}
	switch {
	case r.Method == "PUT" && len(q) == 0:
		if err := admin(); err != nil {
			return err
		}
		return s.Core.CreateBucket(b, strings.EqualFold(r.Header.Get("x-amz-bucket-object-lock-enabled"), "true"))
	case r.Method == "PUT" && has("versioning"):
		if err := admin(); err != nil {
			return err
		}
		var v struct{ Status string }
		if err := xml.NewDecoder(r.Body).Decode(&v); err != nil {
			return &fake.S3Error{Code: "MalformedXML", Status: 400, Msg: err.Error()}
		}
		return s.Core.SetVersioning(b, v.Status == "Enabled")
	case r.Method == "PUT" && has("object-lock"):
		if err := admin(); err != nil {
			return err
		}
		var v struct {
			Rule struct{ DefaultRetention struct{ Days int } }
		}
		if err := xml.NewDecoder(r.Body).Decode(&v); err != nil {
			return &fake.S3Error{Code: "MalformedXML", Status: 400, Msg: err.Error()}
		}
		return s.Core.SetDefaultRetention(b, v.Rule.DefaultRetention.Days)
	case r.Method == "GET" && (has("versioning") || has("object-lock")):
		return s.getBucketConfig(w, b, has("versioning"))
	case r.Method == "GET" && has("versions"):
		return s.listVersions(w, b, r.URL.Query().Get("prefix"))
	default:
		return notImpl(r)
	}
}

func (s *Server) getBucketConfig(w http.ResponseWriter, b string, versioning bool) error {
	ver, lock, days, err := s.Core.Config(b)
	if err != nil {
		return err
	}
	if versioning {
		st := ""
		if ver {
			st = "<Status>Enabled</Status>"
		}
		fmt.Fprintf(w, `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">%s</VersioningConfiguration>`, st)
		return nil
	}
	if !lock {
		return &fake.S3Error{Code: "ObjectLockConfigurationNotFoundError", Status: 404, Msg: b}
	}
	rule := ""
	if days > 0 {
		rule = fmt.Sprintf("<Rule><DefaultRetention><Mode>COMPLIANCE</Mode><Days>%d</Days></DefaultRetention></Rule>", days)
	}
	fmt.Fprintf(w, `<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled>%s</ObjectLockConfiguration>`, rule)
	return nil
}

func (s *Server) listVersions(w http.ResponseWriter, b, prefix string) error {
	objs, err := s.Core.List(b, prefix)
	if err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString(`<ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>false</IsTruncated>`)
	seen := map[string]bool{}
	for _, o := range objs {
		tag := "Version"
		if o.DeleteMarker {
			tag = "DeleteMarker"
		}
		fmt.Fprintf(&sb, "<%s><Key>%s</Key><VersionId>%s</VersionId><IsLatest>%v</IsLatest><LastModified>%s</LastModified>",
			tag, xmlEsc(o.Key), o.VersionID, !seen[o.Key], o.Modified.UTC().Format("2006-01-02T15:04:05.000Z"))
		if !o.DeleteMarker {
			fmt.Fprintf(&sb, `<ETag>"%s"</ETag><Size>%d</Size>`, o.ETag, len(o.Body))
		}
		fmt.Fprintf(&sb, "</%s>", tag)
		seen[o.Key] = true
	}
	sb.WriteString("</ListVersionsResult>")
	_, _ = io.WriteString(w, sb.String())
	return nil
}

func xmlEsc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (s *Server) objectOp(w http.ResponseWriter, r *http.Request, b, key string, q map[string][]string, role fake.Role) error {
	ver := ""
	if v := q["versionId"]; len(v) > 0 {
		ver = v[0]
	}
	_, retention := q["retention"]
	switch {
	case retention:
		return s.retentionOp(w, r, b, key, ver, role)
	case r.Method == "PUT" && len(q) == 0:
		return s.put(w, r, b, key, role)
	case (r.Method == "GET" || r.Method == "HEAD") && (len(q) == 0 || ver != ""):
		o, err := s.Core.Get(b, key, ver)
		if err != nil {
			return err
		}
		w.Header().Set("ETag", `"`+o.ETag+`"`)
		w.Header().Set("x-amz-version-id", o.VersionID)
		w.Header().Set("Content-Length", strconv.Itoa(len(o.Body)))
		if !o.RetainUntil.IsZero() {
			w.Header().Set("x-amz-object-lock-mode", "COMPLIANCE")
			w.Header().Set("x-amz-object-lock-retain-until-date", o.RetainUntil.UTC().Format("2006-01-02T15:04:05.000Z"))
		}
		if r.Method == "GET" {
			_, _ = w.Write(o.Body)
		}
	case r.Method == "DELETE" && (len(q) == 0 || ver != ""):
		m, err := s.Core.Delete(b, key, ver, role)
		if err != nil {
			return err
		}
		if m != "" {
			w.Header().Set("x-amz-delete-marker", "true")
			w.Header().Set("x-amz-version-id", m)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		return notImpl(r)
	}
	return nil
}

func (s *Server) retentionOp(w http.ResponseWriter, r *http.Request, b, key, ver string, role fake.Role) error {
	if r.Method == "GET" {
		t, err := s.Core.GetRetention(b, key, ver)
		if err != nil {
			return err
		}
		if t.IsZero() {
			return &fake.S3Error{Code: "NoSuchObjectLockConfiguration", Status: 404, Msg: key}
		}
		fmt.Fprintf(w, `<Retention xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Mode>COMPLIANCE</Mode><RetainUntilDate>%s</RetainUntilDate></Retention>`, t.UTC().Format("2006-01-02T15:04:05.000Z"))
		return nil
	}
	if r.Method != "PUT" {
		return notImpl(r)
	}
	var v struct {
		Mode            string
		RetainUntilDate string
	}
	if err := xml.NewDecoder(r.Body).Decode(&v); err != nil {
		return &fake.S3Error{Code: "MalformedXML", Status: 400, Msg: err.Error()}
	}
	if v.Mode != "COMPLIANCE" {
		return &fake.S3Error{Code: "InvalidRequest", Status: 400, Msg: "Mode は COMPLIANCE だけ(GOVERNANCE・無指定は拒否)"}
	}
	t, err := time.Parse(time.RFC3339, v.RetainUntilDate)
	if err != nil {
		return &fake.S3Error{Code: "InvalidArgument", Status: 400, Msg: err.Error()}
	}
	return s.Core.PutRetention(b, key, ver, t, role)
}

func (s *Server) put(w http.ResponseWriter, r *http.Request, b, key string, role fake.Role) error {
	if role == fake.RoleRead {
		return &fake.S3Error{Code: "AccessDenied", Status: 403, Msg: "読み取り鍵は書けない"}
	}
	body, err := readBody(r)
	if err != nil {
		return &fake.S3Error{Code: "IncompleteBody", Status: 400, Msg: err.Error()}
	}
	o := fake.PutOptions{}
	if md5b := r.Header.Get("Content-MD5"); md5b != "" {
		raw, err := base64.StdEncoding.DecodeString(md5b)
		if err != nil {
			return &fake.S3Error{Code: "InvalidDigest", Status: 400, Msg: "Content-MD5"}
		}
		o.MD5 = hex.EncodeToString(raw)
	}
	if v := r.Header.Get("x-amz-object-lock-retain-until-date"); v != "" {
		if r.Header.Get("x-amz-object-lock-mode") != "COMPLIANCE" {
			return &fake.S3Error{Code: "InvalidRequest", Status: 400, Msg: "Mode は COMPLIANCE だけ(GOVERNANCE・無指定は拒否)"}
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return &fake.S3Error{Code: "InvalidArgument", Status: 400, Msg: "retain-until"}
		}
		o.RetainUntil = t
	}
	obj, err := s.Core.Put(b, key, body, o)
	if err != nil {
		return err
	}
	w.Header().Set("ETag", `"`+obj.ETag+`"`)
	w.Header().Set("x-amz-version-id", obj.VersionID)
	return nil
}

// readBody は本体を読む。aws-chunked(STREAMING-*)なら断片を外す。
func readBody(r *http.Request) ([]byte, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(r.Header.Get("x-amz-content-sha256"), "STREAMING-") {
		return raw, nil
	}
	var out []byte
	for len(raw) > 0 {
		i := bytes.Index(raw, []byte("\r\n"))
		if i < 0 {
			return nil, errors.New("aws-chunked の形が壊れている")
		}
		szs, _, _ := strings.Cut(string(raw[:i]), ";")
		n, err := strconv.ParseInt(szs, 16, 64)
		if err != nil {
			return nil, err
		}
		raw = raw[i+2:]
		if n == 0 {
			break
		}
		if int64(len(raw)) < n+2 {
			return nil, errors.New("aws-chunked が短い")
		}
		out = append(out, raw[:n]...)
		raw = raw[n+2:]
	}
	return out, nil
}
