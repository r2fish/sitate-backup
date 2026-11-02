package keys

// 保管先準備アダプタ(kind: provision)の呼び出し(spec 3.2.6)。発行は全部か無しか。管理用鍵は要求の secrets に入れ、保存しない。

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const provisionContract = "sitate.provision/v1"

// provisionCall は保管先準備アダプタを 1 回呼ぶ。
func provisionCall(ctx context.Context, run contract.AdapterRunner, name string, req map[string]any) (json.RawMessage, error) {
	if run == nil {
		return nil, errInternal()
	}
	req["contract"] = provisionContract
	b, err := json.Marshal(req)
	if err != nil {
		return nil, errInternal()
	}
	return run.Run(ctx, contract.RunContext{ProcessGroup: true, PdeathSig: true}, "provision", name, b, nil)
}

// slotSecrets は管理用鍵・試験する鍵(JSON `{"key_id","secret"}`)を要求の secrets の枠に入れる。値は呼び出しの間だけ持つ。
func slotSecrets(slot string, s *contract.Secret) map[string]any {
	if s == nil {
		return map[string]any{}
	}
	return map[string]any{slot: json.RawMessage(s.Reveal())}
}

// IssueKeys は issue-keys を 1 回呼び、roles の順に鍵を返す。要求の役割の一部しか発行されなければ、発行された鍵を失効させて誤りを返す。
func (s *KeyService) IssueKeys(ctx context.Context, req contract.IssueRequest) ([]contract.IssuedKey, []string, error) {
	out, err := provisionCall(ctx, s.Provision, s.ProvisionName, map[string]any{
		"op": "issue-keys", "endpoint": req.Endpoint, "bucket": req.Bucket, "prefix": req.Prefix, "roles": req.Roles,
		"max_retention_days": req.MaxRetentionDays, "secrets": slotSecrets("admin", req.Admin),
	})
	if err != nil {
		// アダプタの error.orphaned_key_ids[](Error.Adapter)を orphaned に運ぶ。核が写す先のコードを問わず台帳に記録する。
		var ce *contract.Error
		if errors.As(err, &ce) && ce.Adapter != nil {
			return nil, ce.Adapter.OrphanedKeyIDs, err
		}
		return nil, nil, err
	}
	var res struct {
		Issued []struct {
			Role   string `json:"role"`
			KeyID  string `json:"key_id"`
			Secret string `json:"secret"`
		} `json:"issued"`
	}
	if json.Unmarshal(out, &res) != nil {
		return nil, nil, errInternal()
	}
	got := make([]contract.IssuedKey, 0, len(res.Issued))
	ids := make([]string, 0, len(res.Issued))
	roles := make([]string, 0, len(res.Issued))
	for _, i := range res.Issued {
		got = append(got, contract.IssuedKey{Role: i.Role, KeyID: i.KeyID, Secret: contract.NewSecret([]byte(i.Secret))})
		ids = append(ids, i.KeyID)
		roles = append(roles, i.Role)
	}
	if !slices.Equal(roles, req.Roles) {
		for _, g := range got {
			g.Secret.Wipe()
		}
		return nil, s.revokeAfterPartial(ctx, req, ids), mkErr(contract.CodeAdapterError, map[string]string{"kind": "provision", "name": s.ProvisionName, "adapter_code": "ISSUE_INCOMPLETE"})
	}
	return got, nil, nil
}

// revokeAfterPartial は部分的に発行された鍵を失効させる。失効に失敗した鍵(と、確かめられなかった鍵)の ID を返す。
func (s *KeyService) revokeAfterPartial(ctx context.Context, req contract.IssueRequest, ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	rev, nf, err := s.RevokeKey(ctx, contract.RevokeRequest{Endpoint: req.Endpoint, Bucket: req.Bucket, KeyIDs: ids, Admin: req.Admin})
	if err != nil {
		return ids
	}
	var left []string // 失効した・存在しないと確かめられなかった鍵は孤児として返す
	for _, id := range ids {
		if !slices.Contains(rev, id) && !slices.Contains(nf, id) {
			left = append(left, id)
		}
	}
	return left
}

// RevokeKey は revoke-keys を 1 回呼ぶ。Roles が空なら要求に roles を入れない。
func (s *KeyService) RevokeKey(ctx context.Context, req contract.RevokeRequest) (revoked, notFound []string, err error) {
	m := map[string]any{"op": "revoke-keys", "endpoint": req.Endpoint, "bucket": req.Bucket, "key_ids": req.KeyIDs, "secrets": slotSecrets("admin", req.Admin)}
	if len(req.Roles) > 0 {
		m["roles"] = req.Roles
	}
	out, err := provisionCall(ctx, s.Provision, s.ProvisionName, m)
	if err != nil {
		return nil, nil, err
	}
	var res struct {
		Revoked  []string `json:"revoked"`
		NotFound []string `json:"not_found"`
	}
	if json.Unmarshal(out, &res) != nil {
		return nil, nil, errInternal()
	}
	return res.Revoked, res.NotFound, nil
}

// checkKeys は枠 admin の check を呼び、製品の印の付いた鍵(key_id の列)を返す(発行の後の照合・旧鍵の特定)。
func checkAdmin(ctx context.Context, run contract.AdapterRunner, name string, endpoint, bucket, prefix string, admin *contract.Secret) (keyIDs []string, err error) {
	out, err := provisionCall(ctx, run, name, map[string]any{"op": "check", "endpoint": endpoint, "bucket": bucket, "prefix": prefix, "key_role": nil, "secrets": slotSecrets("admin", admin)})
	if err != nil {
		return nil, err
	}
	var res struct {
		Keys []struct {
			KeyID string `json:"key_id"`
		} `json:"keys"`
	}
	if json.Unmarshal(out, &res) != nil {
		return nil, errInternal()
	}
	for _, k := range res.Keys {
		keyIDs = append(keyIDs, k.KeyID)
	}
	return keyIDs, nil
}

// capsFrame は枠 key の check の結果(その鍵の capabilities)。Caps が nil なら鍵が自分の権限を読めない。
type capsFrame struct {
	Caps []string
	Null bool
}

func checkKey(ctx context.Context, run contract.AdapterRunner, name, endpoint, bucket, prefix, role string, key *contract.Secret) (capsFrame, error) {
	out, err := provisionCall(ctx, run, name, map[string]any{"op": "check", "endpoint": endpoint, "bucket": bucket, "prefix": prefix, "key_role": role, "secrets": slotSecrets("key", key)})
	if err != nil {
		return capsFrame{}, err
	}
	var res struct {
		Keys []struct {
			Capabilities []string `json:"capabilities"`
		} `json:"keys"`
	}
	if json.Unmarshal(out, &res) != nil || len(res.Keys) == 0 {
		return capsFrame{}, errInternal()
	}
	if res.Keys[0].Capabilities == nil {
		return capsFrame{Null: true}, nil
	}
	return capsFrame{Caps: res.Keys[0].Capabilities}, nil
}
