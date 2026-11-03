package store

// 実物の ObjectStore の試験(偽の保管先のサーバー + 代理)。契約試験が緩い規則(書き込みのたびの retain-until の明示、Content-MD5、再試行、時計のずれ)を
// 要求の中身で確かめる。MinIO に対する同じ判定は minio_test.go。

import (
	"context"
	"crypto/md5" //nolint:gosec // Content-MD5 の照合
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// 書き込みのたびに retain-until(Compliance、応答の Date + 日次 90 日)を明示し、Content-MD5 を本体から付ける(spec 4.2)。
func TestPutExplicitRetainUntilAndContentMD5(t *testing.T) {
	px := newProxied(t)
	date := px.Clock.T
	r, err := px.S.Put(ctx, "chunks/a", strings.NewReader("hello"), contract.RetentionDaily)
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	puts := px.P.puts()
	if len(puts) != 1 {
		t.Fatalf("PutObject の要求は 1 件のはず: %d", len(puts))
	}
	h := puts[0].Header
	sum := md5.Sum([]byte("hello")) //nolint:gosec
	if got, want := h.Get("Content-MD5"), base64.StdEncoding.EncodeToString(sum[:]); got != want {
		t.Errorf("Content-MD5 = %q, 本体の MD5 は %q", got, want)
	}
	if h.Get("X-Amz-Object-Lock-Mode") != "COMPLIANCE" {
		t.Errorf("方式は COMPLIANCE の明示: %q", h.Get("X-Amz-Object-Lock-Mode"))
	}
	until, perr := time.Parse(time.RFC3339, h.Get("X-Amz-Object-Lock-Retain-Until-Date"))
	if perr != nil {
		t.Fatalf("retain-until が要求に無い・読めない: %q", h.Get("X-Amz-Object-Lock-Retain-Until-Date"))
	}
	want := date.AddDate(0, 0, contract.RetentionDailyDays)
	if !until.Equal(want) || !r.RetainUntil.Equal(want) {
		t.Errorf("retain-until = 要求 %v / 応答 %v, 期待 %v(応答の Date + 日次の保持日数)", until, r.RetainUntil, want)
	}
}

// 月次は「Date + 12 か月 + 7 日」(月末は丸める)。spec 4.2 の書き込みの retain-until。
func TestPutMonthlyRetainUntil(t *testing.T) {
	px := newProxied(t)
	px.Clock.T = time.Date(2028, 2, 29, 3, 0, 0, 0, time.UTC)
	r, err := px.S.Put(ctx, "manifests/m-x", strings.NewReader("m"), contract.RetentionMonthly)
	harness.Implemented(t, err)
	want := time.Date(2029, 2, 28, 3, 0, 0, 0, time.UTC).Add(7 * 24 * time.Hour)
	if err != nil || !r.RetainUntil.Equal(want) {
		t.Errorf("月次: %v %v 期待 %v", r.RetainUntil, err, want)
	}
}

// その実行でまだ応答が無ければ、最初の書き込みの前にバケット設定を 1 回読んで Date を得る(再試行では前の応答の Date から計算し直す)。
func TestPutReadsBucketConfigFirstThenRecomputesOnRetry(t *testing.T) {
	px := newProxied(t)
	d1 := px.Clock.T
	d2 := d1.Add(2 * time.Hour)
	px.P.date = func() time.Time {
		px.P.mu.Lock()
		defer px.P.mu.Unlock()
		if len(px.P.reqs) > 2 { // 3 件目(503 の要求)の応答から 2 時間後の Date
			return d2
		}
		return d1
	}
	px.S.Clock = clockFn(func() time.Time { return px.P.date() }) // 時計は保管先の模擬時間に追従する(ずれを見たいのではない)
	px.P.hook = func(n int, r *http.Request, w http.ResponseWriter) int {
		if r.Method == "PUT" && n == 2 { // 1 回目の書き込み(0,1 番目はバケット設定の読み取り)は 503
			return http.StatusServiceUnavailable
		}
		return 0
	}
	r, err := px.S.Put(ctx, "chunks/a", strings.NewReader("x"), contract.RetentionDaily)
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	px.P.mu.Lock()
	first := px.P.reqs[0]
	px.P.mu.Unlock()
	if first.Method != "GET" || !strings.Contains(first.Path, "versioning") {
		t.Errorf("最初の要求はバケット設定の読み取り: %s %s", first.Method, first.Path)
	}
	puts := px.P.puts()
	if len(puts) != 2 {
		t.Fatalf("書き込みの要求は 503 と再試行の 2 件: %d", len(puts))
	}
	u1, _ := time.Parse(time.RFC3339, puts[0].Header.Get("X-Amz-Object-Lock-Retain-Until-Date"))
	u2, _ := time.Parse(time.RFC3339, puts[1].Header.Get("X-Amz-Object-Lock-Retain-Until-Date"))
	if !u1.Equal(d1.AddDate(0, 0, contract.RetentionDailyDays)) || !u2.Equal(d2.AddDate(0, 0, contract.RetentionDailyDays)) || !r.RetainUntil.Equal(u2) {
		t.Errorf("再試行の retain-until は直前の応答の Date から計算し直す: 1 回目 %v 2 回目 %v 結果 %v", u1, u2, r.RetainUntil)
	}
	if puts[1].Header.Get("Content-MD5") == "" {
		t.Error("再試行にも Content-MD5 を付ける")
	}
}

// 再試行: 5xx・429 は STORE_RETRY_MAX まで指数バックオフで再試行し、使い切ったら STORAGE_UNREACHABLE。403 は再試行しない。Retry-After は待ちの長さ。
func TestRetryPolicy(t *testing.T) {
	px := newProxied(t)
	var slept []time.Duration
	px.S.sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	px.P.hook = func(n int, r *http.Request, w http.ResponseWriter) int {
		if r.Method == "PUT" {
			return http.StatusInternalServerError
		}
		return 0
	}
	_, err := px.S.Put(ctx, "chunks/a", strings.NewReader("x"), contract.RetentionDaily)
	ce := harness.ExpectCode(t, err, contract.CodeStorageUnreachable)
	if n := len(px.P.puts()); n != 1+contract.StoreRetryMax {
		t.Errorf("要求の数 = %d, 期待 %d", n, 1+contract.StoreRetryMax)
	}
	if len(slept) != contract.StoreRetryMax || slept[0] != contract.StoreBackoffBase || slept[1] != 2*contract.StoreBackoffBase || slept[4] != 16*contract.StoreBackoffBase {
		t.Errorf("バックオフ: %v", slept)
	}
	if !strings.Contains(string(ce.Detail), `"server_error"`) {
		t.Errorf("detail: %s", ce.Detail)
	}
}

func TestRetryNotOn403AndRetryAfter(t *testing.T) {
	px := newProxied(t)
	px.P.hook = func(n int, r *http.Request, w http.ResponseWriter) int {
		if r.Method == "PUT" {
			return http.StatusForbidden
		}
		return 0
	}
	_, err := px.S.Put(ctx, "chunks/a", strings.NewReader("x"), contract.RetentionDaily)
	harness.ExpectCode(t, err, contract.CodeStorageDenied)
	if n := len(px.P.puts()); n != 1 {
		t.Errorf("403 は再試行しない: %d 回", n)
	}
	px = newProxied(t)
	var slept []time.Duration
	px.S.sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	px.P.hook = func(n int, r *http.Request, w http.ResponseWriter) int {
		if r.Method == "PUT" && n < 3 {
			w.Header().Set("Retry-After", "7")
			return http.StatusTooManyRequests
		}
		return 0
	}
	if _, err := px.S.Put(ctx, "chunks/a", strings.NewReader("x"), contract.RetentionDaily); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] != 7*time.Second {
		t.Errorf("Retry-After の秒数を待つ: %v", slept)
	}
}

// 同時に飛ぶ要求は STORE_PARALLELISM まで。
func TestParallelismLimit(t *testing.T) {
	px := newProxied(t)
	px.P.delay = 30 * time.Millisecond
	if _, err := px.S.BucketConfig(ctx); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 3*contract.StoreParallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = px.S.Put(ctx, "chunks/p"+string(rune('a'+i)), strings.NewReader("x"), contract.RetentionDaily)
		}()
	}
	wg.Wait()
	if px.P.maxIn > contract.StoreParallelism || px.P.maxIn < 2 {
		t.Errorf("同時の要求の最大 = %d(上限 %d、2 以上は出るはず)", px.P.maxIn, contract.StoreParallelism)
	}
}

type offs time.Duration

func (o offs) Offset() (time.Duration, error) { return time.Duration(o), nil }

// 時計のずれ: 「Date + オフセット」とバックアップサーバーの時計の差が TIME_SKEW_MAX を超えたら、書き込みと保持操作を止める(TIME_SKEW。保管先には書かない)。
func TestTimeSkewStopsWritesAndRetentionOps(t *testing.T) {
	px := newProxied(t)
	ok, err := px.S.Put(ctx, "chunks/ok", strings.NewReader("x"), contract.RetentionDaily)
	if err != nil {
		t.Fatal(err)
	}
	before := px.P.count()
	px.S.Offset = offs(contract.TimeSkewMax + time.Minute) // Date + オフセットが時計より 6 分進む
	_, err = px.S.Put(ctx, "chunks/skew", strings.NewReader("x"), contract.RetentionDaily)
	ce := harness.ExpectCode(t, err, contract.CodeTimeSkew)
	if !strings.Contains(string(ce.Detail), `"max_s":300`) || !strings.Contains(string(ce.Detail), `"skew_s":-`) {
		t.Errorf("detail は skew_s(時計が進んでいれば正。ここは負)と max_s: %s", ce.Detail)
	}
	harness.ExpectCode(t, px.S.Extend(ctx, "chunks/ok", ok.VersionID, ok.RetainUntil.Add(time.Hour)), contract.CodeTimeSkew)
	harness.ExpectCode(t, px.S.DeleteVersion(ctx, "chunks/ok", ok.VersionID), contract.CodeTimeSkew)
	if n := px.P.count(); n != before {
		t.Errorf("TIME_SKEW で止めた操作が保管先へ要求を送った: %d 件", n-before)
	}
	vs, _ := px.S.ListVersions(ctx, "")
	if len(vs) != 1 {
		t.Errorf("記録だけにせず書き込みを止める: %+v", vs)
	}
	// 時計が逆向きに進んでいても同じ(絶対値)。
	px.S.Offset = offs(-(contract.TimeSkewMax + time.Minute))
	_, err = px.S.Put(ctx, "chunks/skew2", strings.NewReader("x"), contract.RetentionDaily)
	harness.ExpectCode(t, err, contract.CodeTimeSkew)
}

// オフセットを与えた時計の偽物: 時計が「Date + オフセット」に追従していれば止めず、retain-until は「Date + オフセット」+ 90 日で計算する。
func TestOffsetClockRetainUntil(t *testing.T) {
	px := newProxied(t)
	off := 100 * 24 * time.Hour
	px.S.Offset = offs(off)
	px.S.Clock = shifted{off}
	r, err := px.S.Put(ctx, "chunks/a", strings.NewReader("x"), contract.RetentionDaily)
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	want := px.Clock.T.Add(off).AddDate(0, 0, contract.RetentionDailyDays)
	if !r.RetainUntil.Equal(want) {
		t.Errorf("retain-until = %v, 期待 Date + オフセット + 90 日 = %v", r.RetainUntil, want)
	}
	now, _ := px.S.StoreTime(ctx)
	if !now.Equal(px.Clock.T) {
		t.Errorf("StoreTime は直前の応答の Date(オフセットは核が足す): %v", now)
	}
	// 小さなずれ(TIME_SKEW_MAX の内)は通る。
	px.S.Offset, px.S.Clock = offs(time.Minute), nil
	if _, err := px.S.Put(ctx, "chunks/b", strings.NewReader("x"), contract.RetentionDaily); err != nil {
		t.Errorf("TIME_SKEW_MAX の内のずれは止めない: %v", err)
	}
}

type shifted struct{ d time.Duration }

func (s shifted) Now(context.Context) (time.Time, error) { return time.Now().UTC().Add(s.d), nil }

// 取り消された文脈の書き込みは保管先へ送らない(RUN_ABORTED)。
func TestPutAbortedWritesNothing(t *testing.T) {
	px := newProxied(t)
	c, cancel := withCancel()
	cancel()
	_, err := px.S.Put(c, "chunks/a", strings.NewReader("x"), contract.RetentionDaily)
	harness.ExpectCode(t, err, contract.CodeRunAborted)
	if px.P.count() != 0 {
		t.Errorf("取り消された書き込みが要求を送った: %d 件", px.P.count())
	}
}

// 版 ID の無い読み出し・削除・延長は使わない。削除マーカーは版指定の読み出しで ErrMissing、一覧には出る。
func TestVersionIDRequiredAndDeleteMarkers(t *testing.T) {
	px := newProxied(t)
	r, err := px.S.Put(ctx, "chunks/a", strings.NewReader("x"), contract.RetentionDaily)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := px.S.Get(ctx, "chunks/a", ""); err == nil {
		t.Error("版 ID の無い読み出しが通った")
	}
	if err := px.S.DeleteVersion(ctx, "chunks/a", ""); err == nil {
		t.Error("版 ID の無い削除が通った")
	}
	if err := px.S.Extend(ctx, "chunks/a", "", r.RetainUntil.Add(time.Hour)); err == nil {
		t.Error("版 ID の無い延長が通った")
	}
	if _, err := px.Core.Delete("b", "chunks/a", "", "admin"); err != nil {
		t.Fatal(err)
	}
	vs, err := px.S.ListVersions(ctx, "chunks/")
	if err != nil {
		t.Fatal(err)
	}
	markers := 0
	for _, v := range vs {
		if v.DeleteMarker {
			markers++
		}
	}
	if len(vs) != 2 || markers != 1 {
		t.Errorf("削除マーカーを含む一覧: %+v", vs)
	}
	if rc, err := px.S.Get(ctx, "chunks/a", r.VersionID); err != nil {
		t.Errorf("版指定の読み出しは削除マーカーを無視する: %v", err)
	} else {
		rc.Close()
	}
	for _, v := range vs {
		if v.DeleteMarker {
			if _, err := px.S.Get(ctx, "chunks/a", v.VersionID); !errors.Is(err, contract.ErrMissing) {
				t.Errorf("削除マーカーの版は ErrMissing: %v", err)
			}
		}
	}
}

func withCancel() (context.Context, context.CancelFunc) { return context.WithCancel(ctx) }

type clockFn func() time.Time

func (c clockFn) Now(context.Context) (time.Time, error) { return c(), nil }

// spec 3.2.6: 「拒否されれば理由を問わず合格」は retention_shorten の行だけ。保管先が既知の符号(InvalidArgument など)の 4xx で拒んだら拒否として扱う。
// 権限以外の 4xx(形式誤り・404)・符号の無い 4xx・5xx は拒否にしない。legal_hold と unversioned_delete は符号が既知でも拒否と読まない(権限の拒否だけ)。
func TestProbeTriedOps4xxIsDenied(t *testing.T) {
	px := newProxied(t)
	p := &StoreProbe{Keys: map[string]contract.ObjectStore{"write": px.S}}
	if _, err := p.PutProbe(ctx, "probe/x", []byte("p")); err != nil {
		t.Fatal(err)
	}
	status := 400
	px.P.hookBody = "<Error><Code>InvalidArgument</Code><Message>x</Message></Error>"
	px.P.hook = func(n int, r *http.Request, w http.ResponseWriter) int {
		q := r.URL.Query()
		_, ret := q["retention"]
		_, lh := q["legal-hold"]
		if r.Method == http.MethodPut && (ret || lh) || r.Method == http.MethodDelete && q.Get("versionId") == "" {
			return status
		}
		return 0
	}
	if ok, err := p.Try(ctx, "write", "retention_shorten"); err != nil || ok {
		t.Errorf("retention_shorten: 既知の符号の 4xx は拒否(false, nil)のはず: %v %v", ok, err)
	}
	for _, op := range []string{"legal_hold", "unversioned_delete"} {
		if ok, err := p.Try(ctx, "write", op); err == nil || ok {
			t.Errorf("%s: 拒否でない 4xx は拒否と読まない(誤りのまま): %v %v", op, ok, err)
		}
	}
	ver, _ := p.last()
	if err := px.S.Extend(ctx, "probe/x", ver, px.Clock.T.Add(48*time.Hour)); err == nil || isDenied(err) {
		t.Errorf("正当な延長の 4xx は誤りのまま: %v", err)
	}
	status = 503
	if ok, err := p.Try(ctx, "write", "retention_shorten"); err == nil || ok {
		t.Errorf("5xx は拒否にしない: %v %v", ok, err)
	}
}

// 権限以外の 4xx(400 の形式誤り・404)では、読み取り鍵の unversioned_delete も retention_shorten も拒否と読まれない。
func TestProbeNonPermission4xxIsNotDenied(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
	}{
		{400, "<Error><Code>MalformedXML</Code></Error>"},
		{404, "<Error><Code>NoSuchBucket</Code></Error>"},
		{400, ""},
	} {
		px := newProxied(t)
		p := &StoreProbe{Keys: map[string]contract.ObjectStore{"read": px.S, "write": px.S}}
		if _, err := p.PutProbe(ctx, "probe/x", []byte("p")); err != nil {
			t.Fatal(err)
		}
		px.P.hookBody = c.body
		px.P.hook = func(n int, r *http.Request, w http.ResponseWriter) int {
			q := r.URL.Query()
			_, ret := q["retention"]
			if r.Method == http.MethodPut && ret || r.Method == http.MethodDelete && q.Get("versionId") == "" {
				return c.status
			}
			return 0
		}
		for _, op := range []string{"unversioned_delete", "retention_shorten"} {
			if ok, err := p.Try(ctx, "read", op); err == nil || ok {
				t.Errorf("%d %q %s: 拒否と読んではならない(誤りのまま): %v %v", c.status, c.body, op, ok, err)
			}
		}
	}
}
