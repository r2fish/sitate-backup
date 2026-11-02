package contract

import (
	"context"
	"errors"
	"io"
	"time"
)

// RetentionClass は保持区分(spec 4.2)。
type RetentionClass string

// 保持区分。
const (
	RetentionDaily   RetentionClass = "daily"
	RetentionMonthly RetentionClass = "monthly"
)

// ErrMissing は版指定の読み出しで版が無いときの誤り。
var ErrMissing = errors.New("object: 版が無い")

// PutResult は書き込みの応答(書き込みの応答で返った版 ID を記録する。spec 4.2)。
type PutResult struct {
	VersionID   string
	Size        int64
	ETag        string
	RetainUntil time.Time
}

// VersionInfo は版付き一覧の要素。
type VersionInfo struct {
	Key          string
	VersionID    string
	LastModified time.Time
	Size         int64
	IsLatest     bool
	DeleteMarker bool
	// RetainUntil は一覧が運べるときだけの値。S3 の版付き一覧は retain-until を返さないので、実物はゼロで返す。読むときは ObjectStore.RetainUntil。
	RetainUntil time.Time
}

// BucketConfig はバケット設定の読み取り結果。
type BucketConfig struct {
	Versioning       bool
	LockMode         string // "COMPLIANCE" など。空なら Object Lock なし。
	DefaultRetention int    // 日
}

// ObjectStore は保管先 I/O(spec 7.5。部品 store)。retain-until と Content-MD5 は呼び出し側から受けずに付ける。
//
// キー(key・prefix・VersionInfo.Key)は spec 4.2 の表の `<p>` を含まない形(`repo.json`・`keyring/<keyring_id>`・`manifests/<ref>` など)で、
// `<p>`(プレフィクス。既定 `sitate/`)は ObjectStore の実物が付ける。ListVersions は `<p>` の下だけを返し、返す Key から `<p>` を外す。
// 呼び出し側(keys・restore・ほか)は `<p>` を付けない・外さない(付けると `sitate/sitate/` と二重になり、鍵の範囲 3.2.6 `issue-keys` の prefix の外へ書いて拒否される)。
type ObjectStore interface {
	Put(ctx context.Context, key string, body io.Reader, class RetentionClass) (PutResult, error)
	Get(ctx context.Context, key, versionID string) (io.ReadCloser, error)
	ListVersions(ctx context.Context, prefix string) ([]VersionInfo, error)
	Extend(ctx context.Context, key, versionID string, retainUntil time.Time) error
	DeleteVersion(ctx context.Context, key, versionID string) error
	BucketConfig(ctx context.Context) (BucketConfig, error)
	// StoreTime は直前の応答の Date(実時刻。偽物も実時刻を返す)。試験ビルドの「Date + 注入したオフセット」は核が計算する(作業指示書 3.3)。
	StoreTime(ctx context.Context) (time.Time, error)
	// RetainUntil は版を指定して現在の retain-until を読む(保持の無い版はゼロ。読めなければ誤りで、ゼロを期限切れと取り違えない)。
	RetainUntil(ctx context.Context, key, versionID string) (time.Time, error)
	// CheckSkew は保管先の Date と手元の時計の差を確かめ、TIME_SKEW_MAX を超えれば TIME_SKEW を返す(保持の操作の始めに使う。spec 4.2)。
	CheckSkew(ctx context.Context) error
}
