// Package fake は internal/contract の偽物(作業指示書 5.1)。製品ビルドの依存には入らない(make build が検査する)。
// このファイルは偽の保管先の核: Object Lock の意味論と制御できる時計を持つ、メモリ上の版付きバケット。
package fake

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

// TimeSource は時刻の源。偽の保管先は Object Lock の期限の判定をこの源で行う(時刻を読むたびに呼ぶ)。
type TimeSource interface{ Now() (time.Time, error) }

// FuncClock は関数を TimeSource にする。
type FuncClock func() (time.Time, error)

// Now は TimeSource を満たす。
func (f FuncClock) Now() (time.Time, error) { return f() }

// S3Error は S3 の形のエラー(コードと HTTP 状態)。
type S3Error struct {
	Code   string
	Status int
	Msg    string
}

func (e *S3Error) Error() string { return fmt.Sprintf("%s(%d): %s", e.Code, e.Status, e.Msg) }

func s3err(code string, status int, msg string) *S3Error { return &S3Error{code, status, msg} }

// Role は鍵の役割(spec 4.2 の鍵の権限の表の模擬)。
type Role string

// 鍵の役割。
const (
	RoleWrite Role = "write"
	RoleRead  Role = "read"
	RoleAdmin Role = "admin"
)

// Object は 1 つの版。
type Object struct {
	Key, VersionID string
	Body           []byte
	ETag           string
	Modified       time.Time
	RetainUntil    time.Time // ゼロなら保持なし
	DeleteMarker   bool
}

type bucket struct {
	lock       bool
	versioning bool
	defDays    int
	objs       map[string][]*Object // キーごとに新しい版が後ろ
}

// Core は偽の保管先の核。
type Core struct {
	mu      sync.Mutex
	clock   TimeSource
	buckets map[string]*bucket
	seq     int
	// AllowUnversionedDelete は書き込み鍵の版指定なしの削除(削除マーカーの作成)を許す設定(KEY_ALLOWS_UNVERSIONED_DELETE の試験用)。
	AllowUnversionedDelete bool
}

// NewCore は Core を作る。
func NewCore(c TimeSource) *Core { return &Core{clock: c, buckets: map[string]*bucket{}} }

func (c *Core) now() (time.Time, error) {
	t, err := c.clock.Now()
	if err != nil {
		return t, s3err("InternalError", 500, "模擬時間の共有の値が読めない: "+err.Error())
	}
	return t, nil
}

// Healthy は模擬時間の共有の値が読めるかを返す。読めないときは S3 の 5xx 相当(InternalError)。すべての操作の前に呼ぶ。
func (c *Core) Healthy() error { _, err := c.now(); return err }

func (c *Core) bucket(name string) (*bucket, error) {
	b, ok := c.buckets[name]
	if !ok {
		return nil, s3err("NoSuchBucket", 404, name)
	}
	return b, nil
}

// CreateBucket はバケットを作る(Object Lock を有効にするとバージョニングも有効)。
func (c *Core) CreateBucket(name string, lock bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.buckets[name]; ok {
		return s3err("BucketAlreadyOwnedByYou", 409, name)
	}
	c.buckets[name] = &bucket{lock: lock, versioning: lock, objs: map[string][]*Object{}}
	return nil
}

// SetVersioning はバージョニングを設定する。
func (c *Core) SetVersioning(name string, on bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := c.bucket(name)
	if err != nil {
		return err
	}
	if b.lock && !on {
		return s3err("InvalidBucketState", 409, "Object Lock のバケットは中断できない")
	}
	b.versioning = on
	return nil
}

// SetDefaultRetention は既定保持(日)を設定する。Object Lock が有効なバケットだけ。
func (c *Core) SetDefaultRetention(name string, days int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := c.bucket(name)
	if err != nil {
		return err
	}
	if !b.lock {
		return s3err("InvalidBucketState", 409, "Object Lock が無効")
	}
	b.defDays = days
	return nil
}

// Config はバケット設定を返す。
func (c *Core) Config(name string) (versioning, lock bool, days int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := c.bucket(name)
	if err != nil {
		return false, false, 0, err
	}
	return b.versioning, b.lock, b.defDays, nil
}

// PutOptions は書き込みの付加情報。
type PutOptions struct {
	RetainUntil time.Time // ゼロなら既定保持
	MD5         string    // 16 進。空なら照合しない
}

// Put は書き込み、版を返す。
func (c *Core) Put(name, key string, body []byte, o PutOptions) (*Object, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := c.bucket(name)
	if err != nil {
		return nil, err
	}
	now, err := c.now()
	if err != nil {
		return nil, err
	}
	sum := md5.Sum(body)
	etag := hex.EncodeToString(sum[:])
	if o.MD5 != "" && o.MD5 != etag {
		return nil, s3err("BadDigest", 400, "Content-MD5 が合わない")
	}
	ru := o.RetainUntil
	if ru.IsZero() && b.lock && b.defDays > 0 {
		ru = now.Add(time.Duration(b.defDays) * 24 * time.Hour)
	}
	if !o.RetainUntil.IsZero() && !b.lock {
		return nil, s3err("InvalidRequest", 400, "Object Lock が無効なバケットに保持を付けられない")
	}
	c.seq++
	o2 := &Object{Key: key, VersionID: fmt.Sprintf("v%08d", c.seq), Body: append([]byte(nil), body...), ETag: etag, Modified: now, RetainUntil: ru}
	if !b.versioning {
		o2.VersionID = "null"
		b.objs[key] = nil
	}
	b.objs[key] = append(b.objs[key], o2)
	return o2, nil
}

func (b *bucket) find(key, version string) (*Object, int) {
	vs := b.objs[key]
	if version == "" {
		for i := len(vs) - 1; i >= 0; i-- {
			return vs[i], i
		}
		return nil, -1
	}
	for i, o := range vs {
		if o.VersionID == version {
			return o, i
		}
	}
	return nil, -1
}

// Get は版を返す(version が空なら最新。削除マーカーが最新なら NoSuchKey)。
func (c *Core) Get(name, key, version string) (*Object, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := c.bucket(name)
	if err != nil {
		return nil, err
	}
	o, _ := b.find(key, version)
	if o == nil || (o.DeleteMarker && version == "") {
		return nil, s3err("NoSuchKey", 404, key)
	}
	if o.DeleteMarker {
		return nil, s3err("MethodNotAllowed", 405, "削除マーカー")
	}
	return o, nil
}

// List は版付き一覧(キー順、同じキーは新しい版が先)。
func (c *Core) List(name, prefix string) ([]*Object, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := c.bucket(name)
	if err != nil {
		return nil, err
	}
	var keys []string
	for k := range b.objs {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var r []*Object
	for _, k := range keys {
		vs := b.objs[k]
		for i := len(vs) - 1; i >= 0; i-- {
			r = append(r, vs[i])
		}
	}
	return r, nil
}
