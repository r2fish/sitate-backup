package restore

import (
	"context"
	"encoding/json"
	"net"
	"net/url"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// defDoc は承認済みの定義の、復元が読む項目。
type defDoc struct {
	Restore *struct {
		Adapter string  `json:"adapter"`
		Dest    destRef `json:"dest"`
		SSHFrom string  `json:"ssh_from"`
	} `json:"restore"`
	Storage []struct {
		Endpoint string `json:"endpoint"`
		Bucket   string `json:"bucket"`
		Prefix   string `json:"prefix"`
		KeyRef   string `json:"key_ref"`
	} `json:"storage"`
	Source struct {
		At   string `json:"at"`
		Host string `json:"host"`
	} `json:"source"`
}

func parseDef(def []byte) (*defDoc, error) {
	var d defDoc
	if json.Unmarshal(def, &d) != nil || d.Restore == nil {
		return nil, newErr(contract.CodeDefInvalid, map[string]any{"issues": []map[string]string{{"pointer": "/restore", "reason": "missing"}}})
	}
	if d.Restore.Adapter == "" {
		d.Restore.Adapter = "ssh-host"
	}
	return &d, nil
}

func (r *Restore) readDef(ctx context.Context) (*defDoc, []byte, error) {
	def, _, err := r.State.ReadDef(ctx)
	if err != nil {
		return nil, nil, err
	}
	d, err := parseDef(def)
	return d, def, err
}

// endpointHost は保管先のエンドポイントの URL のホスト部(egress_host)。
func (d *defDoc) endpointHost() string {
	if len(d.Storage) == 0 {
		return ""
	}
	u, err := url.Parse(d.Storage[0].Endpoint)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// forbiddenNames は復元先が持ってはいけない名前(元サーバーのホスト名。読めなければ無し)。
func (d *defDoc) forbiddenNames() []string {
	if d.Source.Host != "" {
		return []string{d.Source.Host}
	}
	if h, _, ok := parseDestAt(d.Source.At); ok {
		return []string{h}
	}
	return nil
}

// resolveEgress は保管先のエンドポイントを名前解決した IP アドレスの一覧(登録時に固定する送信の許可先。spec 3.2.4)。エンドポイントが無ければ空。
func resolveEgress(ctx context.Context, d *defDoc) ([]string, error) {
	h := d.endpointHost()
	if h == "" {
		return []string{}, nil
	}
	if ip := net.ParseIP(h); ip != nil {
		return []string{ip.String()}, nil
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, h)
	if err != nil || len(addrs) == 0 {
		return nil, newErr(contract.CodeStorageUnreachable, map[string]any{"target": "storage", "reason": "dns", "http_status": nil})
	}
	return addrs, nil
}
