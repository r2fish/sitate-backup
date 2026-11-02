package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/keys"
)

// 結線の試験(主経路の第 2 段で見つけた誤り): bucket check の版指定なしの削除・リーガルホールドの試しは実物の *store.ObjectStore を要する。
// dynStore をそのまま store.StoreProbe に渡すと type assertion が通らず INTERNAL(store、reason: contract)になった。
func TestWiredProbeTriesUnversionedDeleteAndLegalHold(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		switch {
		case r.Method == http.MethodPut && strings.Contains(r.URL.RawQuery, "legal-hold"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut:
			w.Header().Set("x-amz-version-id", "v1")
			w.Header().Set("ETag", `"e"`)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete:
			w.Header().Set("x-amz-delete-marker", "true")
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	d := DefaultDeps(t.TempDir())
	def, _ := json.Marshal(map[string]any{"storage": []any{map[string]any{"endpoint": srv.URL, "bucket": "b", "key_ref": "storage", "prefix": "p/"}}})
	st := &fake.StateFiles{Def: def}
	st.Items = map[string]json.RawMessage{"sealed": json.RawMessage("true")}
	d.State = st
	sk, _ := json.Marshal(map[string]any{"kind": "storage_keys", "keys": []map[string]string{
		{"role": "write", "key_id": "wk", "secret": "ws"}, {"role": "read", "key_id": "rk", "secret": "rs"}}})
	if err := d.Secrets.Put(context.Background(), "storage", sk); err != nil {
		t.Fatal(err)
	}
	probe := d.Keys.(*keys.Keys).Probe
	ctx := withCall(context.Background(), contract.Call{Words: []string{"bucket", "check"}})
	if _, err := probe.PutProbe(ctx, "probe/x", []byte("p")); err != nil {
		t.Fatalf("PutProbe: %v", err)
	}
	for _, op := range []string{"unversioned_delete", "legal_hold"} {
		if _, err := probe.Try(ctx, "write", op); err != nil {
			t.Fatalf("Try(%s): %v(受け取った要求: %v)", op, err, seen)
		}
	}
}
