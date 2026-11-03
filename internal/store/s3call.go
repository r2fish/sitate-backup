package store

// S3 への要求の共通部分: クライアントの組み立て、応答の Date の記録、再試行と並列の上限、誤りの分類(spec 4.2「保管先への要求の再試行と並列」)。

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// dateTransport は応答の Date ヘッダー(1 秒単位)を記録する。保管先の時刻を読める応答ヘッダーはこれだけ(spec 4.2)。
type dateTransport struct {
	base http.RoundTripper
	s    *ObjectStore
}

func (d dateTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := d.base.RoundTrip(r)
	if err == nil {
		if t, perr := http.ParseTime(resp.Header.Get("Date")); perr == nil {
			d.s.setDate(t.UTC())
		}
	}
	return resp, err
}

func (s *ObjectStore) setDate(t time.Time) {
	s.mu.Lock()
	s.date, s.haveDate, s.dateAt = t, true, s.wallNow()
	s.mu.Unlock()
}

// lastDate は直前の応答の Date(まだ応答が無ければ ok=false)。
func (s *ObjectStore) lastDate() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.date, s.haveDate
}

func (s *ObjectStore) setup() {
	s.once.Do(func() {
		region := s.Region
		if region == "" {
			region = "us-east-1"
		}
		hc := &http.Client{Transport: dateTransport{base: http.DefaultTransport, s: s}}
		s.client = s3.New(s3.Options{
			Region: region, RetryMaxAttempts: 1, BaseEndpoint: aws.String(s.Endpoint), UsePathStyle: true, HTTPClient: hc,
			Credentials:                aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(s.AccessKey, s.SecretKey, "")),
			RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
			ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		})
		s.sem = make(chan struct{}, contract.StoreParallelism)
	})
}

func (s *ObjectStore) ready() error {
	if s.Endpoint == "" || s.Bucket == "" || s.Params == nil {
		return nilDep()
	}
	s.setup()
	return nil
}

func (s *ObjectStore) doSleep(ctx context.Context, d time.Duration) error {
	if s.sleep != nil {
		return s.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (s *ObjectStore) reqTimeout() time.Duration {
	if s.timeout > 0 {
		return s.timeout
	}
	return contract.StoreRequestTimeout
}

// call は 1 つの要求を再試行つきで行う。op は試行ごとに呼ばれ(retain-until などを計算し直せる)、文脈は要求ごとのタイムアウトつき。
// 同時に飛ぶ要求は StoreParallelism まで。再試行を使い切ったら STORAGE_UNREACHABLE。
func (s *ObjectStore) call(ctx context.Context, op func(ctx context.Context) error) error {
	for attempt := 0; ; attempt++ {
		if err := aborted(ctx); err != nil {
			return err
		}
		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			return aborted(ctx)
		}
		actx, cancel := context.WithTimeout(ctx, s.reqTimeout())
		err := op(actx)
		cancel()
		<-s.sem
		if err == nil {
			return nil
		}
		if ab := aborted(ctx); ab != nil {
			return ab
		}
		mapped, retry, wait := s.classify(err)
		if !retry || attempt >= contract.StoreRetryMax {
			return mapped
		}
		if wait == 0 {
			wait = contract.StoreBackoffBase << attempt
		}
		if serr := s.doSleep(ctx, wait); serr != nil {
			return aborted(ctx)
		}
	}
}

// classify は S3 の誤りを核の誤りにする。retry は再試行する種類か、wait は Retry-After があればその長さ。
func (s *ObjectStore) classify(err error) (mapped error, retry bool, wait time.Duration) {
	var ce *contract.Error
	if errors.As(err, &ce) {
		return ce, false, 0
	}
	var re *awshttp.ResponseError
	status := 0
	if errors.As(err, &re) {
		status = re.HTTPStatusCode()
	}
	code := ""
	var ae smithy.APIError
	if errors.As(err, &ae) {
		code = ae.ErrorCode()
	}
	switch {
	case code == "NoSuchKey" || code == "NoSuchVersion" || status == http.StatusNotFound && code != "NoSuchBucket" && code != "NoSuchObjectLockConfiguration" && code != "ObjectLockConfigurationNotFoundError":
		return contract.ErrMissing, false, 0
	case code == "MethodNotAllowed": // 版指定の読み出しが削除マーカーに当たった
		return contract.ErrMissing, false, 0
	case code == "RequestTimeTooSkewed":
		return newErr(contract.CodeTimeSkew, s.skewDetail()), false, 0
	case code == "BadDigest" || code == "InvalidDigest" || code == "RequestTimeout":
		return unreachable("server_error", status), true, 0
	case status == http.StatusTooManyRequests || code == "SlowDown":
		return unreachable("throttled", status), true, retryAfter(re)
	case status == http.StatusServiceUnavailable:
		return unreachable("server_error", status), true, retryAfter(re)
	case status >= 500:
		return unreachable("server_error", status), true, 0
	case status == http.StatusForbidden || code == "AccessDenied" || code == "InvalidRequest" || code == "InvalidAccessKeyId" || code == "SignatureDoesNotMatch":
		return newErr(contract.CodeStorageDenied, map[string]any{"reason": "unknown", "http_status": status}), false, 0
	case status != 0:
		return &statusErr{ce: internalStatus(status), s3code: code}, false, 0 // 状態コードと符号を残す(probe.go の triedDenied が保持の短縮の試しだけに使う)
	}
	return unreachable(netReason(err), 0), true, 0
}

func unreachable(reason string, status int) error {
	var hs any
	if status != 0 {
		hs = status
	}
	return newErr(contract.CodeStorageUnreachable, map[string]any{"target": "storage", "reason": reason, "http_status": hs})
}

func netReason(err error) string {
	var dns *net.DNSError
	switch {
	case errors.As(err, &dns):
		return "dns"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "connect"
}

func retryAfter(re *awshttp.ResponseError) time.Duration {
	if re == nil || re.Response == nil {
		return 0
	}
	if n, err := strconv.Atoi(re.Response.Header.Get("Retry-After")); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}
