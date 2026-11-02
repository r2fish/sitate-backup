package cli

// 結線: 保管先(contract.ObjectStore の実物 store.ObjectStore)を、呼び出しごとに所在と鍵を解決して使う。
// 所在は引数の --endpoint/--bucket/--prefix(あれば)と、承認済みの定義(seal の前は候補)の storage[0]。
// 鍵は定義の storage[0].key_ref の秘密(kind: storage_keys)から、役割(write / read)で選ぶ。

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/store"
)

type storeTarget struct{ endpoint, bucket, prefix, keyRef string }

// dynStore は contract.ObjectStore。役割 role の鍵で、文脈の呼び出しが指す保管先を開く。
type dynStore struct {
	d    *Deps
	role string // write | read
	mu   sync.Mutex
	open map[string]*store.ObjectStore
}

var _ contract.ObjectStore = (*dynStore)(nil)

func newDynStore(d *Deps, role string) *dynStore {
	return &dynStore{d: d, role: role, open: map[string]*store.ObjectStore{}}
}

func argFirst(c contract.Call, name string) (string, bool) {
	v := c.Args[name]
	if len(v) == 0 {
		return "", false
	}
	return v[0], true
}

// target は呼び出しの引数と定義から所在を決める(keys の storageTarget と同じ順)。
func (d *Deps) target(ctx context.Context) (storeTarget, error) {
	var t storeTarget
	c, _ := callOf(ctx)
	t.endpoint, _ = argFirst(c, contract.ArgEndpoint)
	t.bucket, _ = argFirst(c, contract.ArgBucket)
	px, hasPx := argFirst(c, contract.ArgPrefix)
	t.prefix = px
	var sealed bool
	if raw, err := d.State.Get(ctx, "sealed"); err == nil {
		_ = json.Unmarshal(raw, &sealed)
	}
	var def []byte
	var found bool
	var err error
	if sealed {
		def, _, err = d.State.ReadDef(ctx)
		found = len(def) > 0
	} else {
		def, _, found, err = d.State.ReadCandidate(ctx)
	}
	if err != nil {
		return t, err
	}
	if found {
		var raw struct {
			Storage []struct {
				Endpoint string `json:"endpoint"`
				Bucket   string `json:"bucket"`
				Prefix   string `json:"prefix"`
				KeyRef   string `json:"key_ref"`
			} `json:"storage"`
		}
		if json.Unmarshal(def, &raw) == nil && len(raw.Storage) > 0 {
			s := raw.Storage[0]
			t.keyRef = s.KeyRef
			if t.endpoint == "" {
				t.endpoint = s.Endpoint
			}
			if t.bucket == "" {
				t.bucket = s.Bucket
			}
			if !hasPx {
				t.prefix = s.Prefix
			}
		}
	}
	if t.prefix == "" {
		t.prefix = contract.StoragePrefixDefault
	}
	if !strings.HasSuffix(t.prefix, "/") {
		t.prefix += "/"
	}
	return t, nil
}

// regionOf は S3 互換の端点の名前から署名の領域名を取る(s3.<region>.<ドメイン>。無ければ空 = 既定)。
func regionOf(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	p := strings.Split(u.Hostname(), ".")
	if len(p) >= 4 && p[0] == "s3" {
		return p[1]
	}
	return ""
}

func (s *dynStore) resolve(ctx context.Context) (*store.ObjectStore, error) {
	if s.d.State == nil || s.d.Secrets == nil {
		return nil, contract.ErrNotImplemented("cli.store")
	}
	t, err := s.d.target(ctx)
	if err != nil {
		return nil, err
	}
	if t.endpoint == "" || t.bucket == "" || t.keyRef == "" {
		return nil, newErr(contract.CodeSecretMissing, map[string]any{"ref": t.keyRef})
	}
	raw, err := s.d.Secrets.Get(ctx, t.keyRef)
	var sk contract.StorageKeysSecret
	if err != nil || json.Unmarshal(raw, &sk) != nil || sk.Kind != contract.SecretKindStorageKeys {
		return nil, newErr(contract.CodeSecretMissing, map[string]any{"ref": t.keyRef})
	}
	for _, e := range sk.Keys {
		if e.Role != s.role {
			continue
		}
		id := strings.Join([]string{t.endpoint, t.bucket, t.prefix, e.KeyID}, "\x00")
		s.mu.Lock()
		defer s.mu.Unlock()
		if o, ok := s.open[id]; ok {
			return o, nil
		}
		var off contract.Offsetter
		if o, ok := s.d.Clock.(contract.Offsetter); ok {
			off = o
		}
		o := &store.ObjectStore{Endpoint: t.endpoint, Bucket: t.bucket, AccessKey: e.KeyID, SecretKey: e.Secret, Region: regionOf(t.endpoint),
			Prefix: t.prefix, Params: s.d.Retention, Offset: off, Clock: s.d.Clock}
		s.open[id] = o
		return o, nil
	}
	return nil, newErr(contract.CodeSecretMissing, map[string]any{"ref": t.keyRef})
}

func (s *dynStore) BucketConfig(ctx context.Context) (contract.BucketConfig, error) {
	o, err := s.resolve(ctx)
	if err != nil {
		return contract.BucketConfig{}, err
	}
	return o.BucketConfig(ctx)
}

func (s *dynStore) CheckSkew(ctx context.Context) error {
	o, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return o.CheckSkew(ctx)
}

func (s *dynStore) DeleteVersion(ctx context.Context, key, versionID string) error {
	o, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return o.DeleteVersion(ctx, key, versionID)
}

func (s *dynStore) Extend(ctx context.Context, key, versionID string, until time.Time) error {
	o, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return o.Extend(ctx, key, versionID, until)
}

func (s *dynStore) Get(ctx context.Context, key, versionID string) (io.ReadCloser, error) {
	o, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return o.Get(ctx, key, versionID)
}

func (s *dynStore) ListVersions(ctx context.Context, prefix string) ([]contract.VersionInfo, error) {
	o, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return o.ListVersions(ctx, prefix)
}

func (s *dynStore) Put(ctx context.Context, key string, body io.Reader, class contract.RetentionClass) (contract.PutResult, error) {
	o, err := s.resolve(ctx)
	if err != nil {
		return contract.PutResult{}, err
	}
	return o.Put(ctx, key, body, class)
}

func (s *dynStore) PutNoRetention(ctx context.Context, key string, body []byte) (string, error) {
	o, err := s.resolve(ctx)
	if err != nil {
		return "", err
	}
	return o.PutNoRetention(ctx, key, body)
}

func (s *dynStore) RetainUntil(ctx context.Context, key, versionID string) (time.Time, error) {
	o, err := s.resolve(ctx)
	if err != nil {
		return time.Time{}, err
	}
	return o.RetainUntil(ctx, key, versionID)
}

func (s *dynStore) SetLegalHold(ctx context.Context, key, versionID string, on bool) error {
	o, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return o.SetLegalHold(ctx, key, versionID, on)
}

func (s *dynStore) StoreTime(ctx context.Context) (time.Time, error) {
	o, err := s.resolve(ctx)
	if err != nil {
		return time.Time{}, err
	}
	return o.StoreTime(ctx)
}

func (s *dynStore) UnversionedDelete(ctx context.Context, key string) error {
	o, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return o.UnversionedDelete(ctx, key)
}
