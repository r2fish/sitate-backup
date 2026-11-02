package fake

import (
	"fmt"
	"time"
)

// locked は版が保持の期間内か。
func locked(o *Object, now time.Time) bool {
	return !o.RetainUntil.IsZero() && now.Before(o.RetainUntil)
}

// Delete は削除する。version が空なら削除マーカーを作る(バージョニングが有効なとき)。
// 版指定の削除は、保持の期間内なら管理用鍵・書き込み鍵とも AccessDenied(Compliance 相当。バイパスは無い)。読み取り鍵は常に拒否。
func (c *Core) Delete(name, key, version string, role Role) (marker string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := c.bucket(name)
	if err != nil {
		return "", err
	}
	now, err := c.now()
	if err != nil {
		return "", err
	}
	if version == "" {
		if role == RoleRead || (role == RoleWrite && !c.AllowUnversionedDelete) {
			return "", s3err("AccessDenied", 403, "版指定なしの削除は許されない")
		}
		if !b.versioning {
			b.objs[key] = nil
			return "", nil
		}
		c.seq++
		m := &Object{Key: key, VersionID: fmt.Sprintf("v%08d", c.seq), Modified: now, DeleteMarker: true}
		b.objs[key] = append(b.objs[key], m)
		return m.VersionID, nil
	}
	// 書き込み鍵は版指定の削除を持つ(spec 3.2.6 の delete_version、T-14)。読み取り鍵だけ拒否し、書き込み鍵は保持の判定(下の locked)で止める。
	if role == RoleRead {
		return "", s3err("AccessDenied", 403, "読み取り鍵は削除できない")
	}
	o, i := b.find(key, version)
	if o == nil {
		return "", nil
	}
	if locked(o, now) {
		return "", s3err("AccessDenied", 403, "WORM protected: 保持の期間内")
	}
	b.objs[key] = append(b.objs[key][:i], b.objs[key][i+1:]...)
	return "", nil
}

// PutRetention は保持期限を設定する。延長だけ許し、短縮は AccessDenied。
func (c *Core) PutRetention(name, key, version string, until time.Time, role Role) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := c.bucket(name)
	if err != nil {
		return err
	}
	if role == RoleRead {
		return s3err("AccessDenied", 403, "読み取り鍵は保持を変えられない")
	}
	o, _ := b.find(key, version)
	if o == nil || o.DeleteMarker {
		return s3err("NoSuchVersion", 404, version)
	}
	if !b.lock {
		return s3err("InvalidRequest", 400, "Object Lock が無効")
	}
	if until.Before(o.RetainUntil) {
		return s3err("AccessDenied", 403, "保持の短縮は許されない")
	}
	o.RetainUntil = until
	return nil
}

// GetRetention は保持期限を返す(無ければゼロ)。
func (c *Core) GetRetention(name, key, version string) (time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := c.bucket(name)
	if err != nil {
		return time.Time{}, err
	}
	o, _ := b.find(key, version)
	if o == nil || o.DeleteMarker {
		return time.Time{}, s3err("NoSuchVersion", 404, version)
	}
	return o.RetainUntil, nil
}
