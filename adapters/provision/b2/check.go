package main

import (
	"context"
)

// checkOp は check(spec 3.2.6)。枠 admin は設定と製品の印の付いた鍵の一覧、枠 key は試験する鍵自身の権限だけ(管理用鍵を使わない)。
func checkOp(ctx context.Context, c *client, req *request) (map[string]any, *provErr) {
	if _, ok := req.Secrets["key"]; ok {
		return checkWithKey(ctx, c, req)
	}
	id, secret := req.creds("admin")
	s, perr := c.authorize(ctx, id, secret, req.Endpoint, "admin")
	if perr != nil {
		return nil, perr
	}
	b, perr := s.findBucket(ctx, req.Bucket)
	if perr != nil {
		return nil, perr
	}
	if b == nil {
		return nil, bucketNotOwned("absent")
	}
	keys, perr := s.listAllKeys(ctx)
	if perr != nil {
		return nil, perr
	}
	out := []map[string]any{}
	for i := range keys {
		k := &keys[i]
		role := k.productRole(req.Bucket, b.ID, req.Prefix)
		if role == "" {
			continue
		}
		out = append(out, map[string]any{"role": role, "key_id": k.ID, "prefix": prefixOrNull(k.NamePrefix), "capabilities": opsFromCaps(k.Caps)})
	}
	st := b.state()
	return map[string]any{"versioning": true, "lock_mode": st.mode, "default_retention_days": st.days, "keys": out}, nil
}

// checkWithKey は試験する鍵で b2_authorize_account を呼び、応答にあるその鍵の capability を操作の名前に写す。
// 設定(versioning・lock_mode・default_retention_days)は核が試験する鍵で読むため null。
func checkWithKey(ctx context.Context, c *client, req *request) (map[string]any, *provErr) {
	id, secret := req.creds("key")
	role := *req.KeyRole
	s, perr := c.authorize(ctx, id, secret, req.Endpoint, role)
	if perr != nil {
		return nil, perr
	}
	var pfx *string
	if s.namePfx != "" {
		pfx = &s.namePfx
	}
	var caps any // 鍵が自分の権限を読めなければ null
	if s.caps != nil {
		caps = opsFromCaps(s.caps)
	}
	key := map[string]any{"role": role, "key_id": id, "prefix": prefixOrNull(pfx), "capabilities": caps}
	return map[string]any{"versioning": nil, "lock_mode": nil, "default_retention_days": nil, "keys": []map[string]any{key}}, nil
}

func prefixOrNull(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}
