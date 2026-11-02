package main

import (
	"context"
)

// issueKeys は issue-keys(spec 3.2.6)。発行は全部か無しか: 一部を発行できなかったら、その呼び出しで発行した鍵を失効させてから error を返す。
// 失効にも失敗した鍵は error.orphaned_key_ids[] に入れる。max_retention_days は B2 の鍵の権限に置けないため使わない。
func issueKeys(ctx context.Context, c *client, req *request) (map[string]any, *provErr) {
	names := make([]string, len(req.Roles))
	for i, role := range req.Roles { // 事業者を呼ぶ前に名前を検査する
		n, perr := keyName(req.Bucket, *req.Prefix, role)
		if perr != nil {
			return nil, perr
		}
		names[i] = n
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
	issued := []map[string]any{}
	for i, role := range req.Roles {
		body := map[string]any{"accountId": s.accountID, "keyName": names[i], "capabilities": capsForRole(role), "bucketIds": []string{b.ID}, "namePrefix": *req.Prefix}
		var res struct {
			ID     string `json:"applicationKeyId"`
			Secret string `json:"applicationKey"`
		}
		if perr := s.call(ctx, "b2_create_key", body, &res); perr != nil {
			perr.orphaned = rollback(ctx, s, issued)
			return nil, perr
		}
		issued = append(issued, map[string]any{"role": role, "key_id": res.ID, "secret": res.Secret})
	}
	return map[string]any{"issued": issued}, nil
}

// rollback は発行済みの鍵を失効させ、失効できなかった key_id を返す。
func rollback(ctx context.Context, s *session, issued []map[string]any) []string {
	var left []string
	for _, k := range issued {
		kid := k["key_id"].(string)
		if perr := s.call(ctx, "b2_delete_key", map[string]any{"applicationKeyId": kid}, nil); perr != nil {
			left = append(left, kid)
		}
	}
	return left
}
