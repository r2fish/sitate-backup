package state

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

type capRunner struct {
	*fake.AdapterRunner
	reqs []json.RawMessage
}

func (c *capRunner) Run(ctx context.Context, rc contract.RunContext, kind, name string, req json.RawMessage, f func(contract.Frame) error) (json.RawMessage, error) {
	c.reqs = append(c.reqs, req)
	return c.AdapterRunner.Run(ctx, rc, kind, name, req, f)
}

// 確認語: 宛先へ送る語と、残すハッシュ(塩つき)が対応し、残す値に語そのものを含めない。宛先ごとに別の語。
func TestTestNotifierWordMatchesStoredHash(t *testing.T) {
	ar := fake.NewAdapterRunner()
	ar.Script("notify", notifyOK, notifyOK)
	cr := &capRunner{AdapterRunner: ar}
	tn := &TestNotifier{State: &fake.StateFiles{}, Adapters: cr, Clock: &fake.Clock{T: fake.StartTime}}
	to := []string{"a@example.invalid", "b@example.invalid"}
	hashes, err := tn.Test(ctx, to)
	if err != nil || len(cr.reqs) != 2 {
		t.Fatalf("%v reqs=%d", err, len(cr.reqs))
	}
	words := map[string]bool{}
	for i, addr := range to {
		var req struct {
			Event struct {
				Facts struct {
					Word string `json:"confirmation_word"`
				} `json:"facts"`
			} `json:"event"`
		}
		if json.Unmarshal(cr.reqs[i], &req) != nil || len(strings.Fields(req.Event.Facts.Word)) != 3 {
			t.Fatalf("宛先へ送る確認語(3 語): %s", cr.reqs[i])
		}
		w := req.Event.Facts.Word
		words[w] = true
		saltB64, h, ok := strings.Cut(hashes[addr], "|")
		salt, err := base64.StdEncoding.DecodeString(saltB64)
		if !ok || err != nil || h != wordHash(salt, w) {
			t.Errorf("%s: 残すハッシュが送った語と対応しない: %s", addr, hashes[addr])
		}
		if strings.Contains(hashes[addr], w) {
			t.Errorf("%s: 語そのものを残した", addr)
		}
	}
	if len(words) != 2 {
		t.Errorf("宛先ごとに別の語: %v", words)
	}
}
