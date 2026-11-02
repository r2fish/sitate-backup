package main

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// revokeKeys は revoke-keys(spec 3.2.6)。失効の前に check と同じ一覧を取り、渡された key_id を全件調べる。
// 一覧に無い key_id は事業者を呼ばずに not_found。製品の印が無い鍵・別のバケット・プレフィクスの印・管理用鍵そのもの・
// roles と違う役割の鍵が 1 つでもあれば、1 件も失効させずに ARG_INVALID(key_ids)。
func revokeKeys(ctx context.Context, c *client, req *request) (map[string]any, *provErr) {
	id, secret := req.creds("admin")
	s, perr := c.authorize(ctx, id, secret, req.Endpoint, "admin")
	if perr != nil {
		return nil, perr
	}
	keys, perr := s.listAllKeys(ctx)
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
	revoked, notFound := []string{}, []string{}
	var targets []string
	for _, kid := range dedupe(req.KeyIDs) {
		i := slices.IndexFunc(keys, func(k listedKey) bool { return k.ID == kid })
		if i < 0 {
			notFound = append(notFound, kid)
			continue
		}
		role := keys[i].productRole(req.Bucket, b.ID, nil)
		if kid == id || role == "" || (len(req.Roles) > 0 && !slices.Contains(req.Roles, role)) {
			return nil, argInvalid("key_ids")
		}
		targets = append(targets, kid)
	}
	for _, kid := range targets {
		err := s.callRaw(ctx, "b2_delete_key", map[string]any{"applicationKeyId": kid}, nil)
		var ae *apiError
		switch {
		case err == nil:
			revoked = append(revoked, kid)
		case errors.As(err, &ae) && ae.status == 400 && strings.Contains(strings.ToLower(ae.message), "does not exist"):
			notFound = append(notFound, kid) // 一覧の後に消えた鍵
		default:
			return nil, mapAPIError(err, "admin")
		}
	}
	return map[string]any{"revoked": revoked, "not_found": notFound}, nil
}

func dedupe(in []string) []string {
	out := make([]string, 0, len(in))
	for _, x := range in {
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}
