package recv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 完了条件(作業指示書 5.8): CONNECT を REVOKE した DB で、受け口の selftest が接続の可否と理由を返す
// (spec 3.3 の selftest の targets[]。connect_error: connect_privilege。核が db_connect と db_privileges を区別する材料)。
// root の使い捨て容器と PostgreSQL が要る(SITATE_DESTRUCTIVE_SETUP=1)。

type pgSelf struct {
	ID            string  `json:"id"`
	Connect       bool    `json:"connect"`
	ConnectError  *string `json:"connect_error"`
	Unreadable    *int    `json:"unreadable_tables"`
	LargeObjects  *bool   `json:"large_objects"`
	ServerVersion *int    `json:"server_version_num"`
}

func selfPG(t *testing.T, out string) pgSelf {
	t.Helper()
	if err := contract.Validate("receiver/selftest.json", []byte(out)); err != nil {
		t.Fatalf("selftest が receiver/selftest.json に合わない: %v\n%s", err, out)
	}
	var st struct{ Targets []pgSelf }
	if err := json.Unmarshal([]byte(out), &st); err != nil || len(st.Targets) != 1 {
		t.Fatalf("selftest の targets: %v %s", err, out)
	}
	return st.Targets[0]
}

func TestSelftestConnectRevoked(t *testing.T) {
	destructive(t)
	bin := pgBin(t)
	d := t.TempDir()
	for x := d; x != os.TempDir() && x != "/"; x = filepath.Dir(x) {
		_ = os.Chmod(x, 0o755)
	}
	_, pgd, psql, sup := startPG(t, bin, d, d)
	defer shRoot(t, sup(fmt.Sprintf("%s/pg_ctl -D %s -m immediate stop", bin, pgd)), nil)
	rec := "t2\tpostgresql\tapp\t/run/postgresql\t5432\t\"app\"\t\"/run/postgresql\"\t0"
	rb, _ := os.ReadFile("sitate-recv")
	vars := map[string]string{
		"SITATE_RECEIVER": strings.Replace(string(rb), "SITATE_TARGETS=''", "SITATE_TARGETS="+sq(rec), 1),
		"SITATE_PULL_KEY": testKey, "SITATE_FROM": "203.0.113.9", "SITATE_TARGETS": rec,
		"SITATE_CLUSTERS": "/run/postgresql\t5432\t" + psqlMajor(bin) + "\t" + pgd,
	}
	if _, out, err := runSetupVars(t, vars); err != nil {
		t.Fatalf("source-setup: %s", out)
	}
	if out, err := asRoRecv(t, "selftest"); err != nil || !selfPG(t, out).Connect {
		t.Fatalf("REVOKE の前は接続できる: %v %s", err, out)
	}
	// CONNECT は既定で PUBLIC にある(spec 3.3 は足さない)。PUBLIC と sitate-ro の両方から外す。
	psql("postgres", `revoke connect on database app from public; revoke connect on database app from "sitate-ro"`)
	out, err := asRoRecv(t, "selftest")
	if err != nil {
		t.Fatalf("selftest は接続できなくても終了 0 で材料を返す: %v %s", err, out)
	}
	s := selfPG(t, out)
	if s.Connect || s.ConnectError == nil || *s.ConnectError != "connect_privilege" {
		t.Errorf("REVOKE 済みは connect=false・connect_error=connect_privilege: %+v %s", s, out)
	}
	if s.Unreadable != nil || s.LargeObjects != nil || s.ServerVersion != nil {
		t.Errorf("接続できないときの版と数は null: %+v", s)
	}
	if o, err := asRoRecv(t, "dump-pg t2"); err == nil {
		t.Errorf("REVOKE 済みの DB で dump-pg は非 0: %.30q", o)
	}
	// 対照: 戻せば接続できる(connect_privilege を他の理由と取り違えていない)
	psql("postgres", `grant connect on database app to public`)
	if out, err := asRoRecv(t, "selftest"); err != nil || !selfPG(t, out).Connect {
		t.Errorf("GRANT で戻せば接続できる: %v %s", err, out)
	}
}
