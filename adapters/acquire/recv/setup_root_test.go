package recv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// source-setup と DB のロールの試験。root で、PostgreSQL の道具(initdb・pg_ctl・psql)があるコンテナの中だけで動く
// (使い捨ての容器の中で sitate-ro のアカウントを作る)。そうでなければ skip(未検証の印)。

func shRoot(t *testing.T, script string, stdin []byte) (string, error) {
	t.Helper()
	c := exec.Command("bash", "-c", script)
	c.Stdin = bytes.NewReader(stdin)
	var o bytes.Buffer
	c.Stdout, c.Stderr = &o, &o
	err := c.Run()
	return o.String(), err
}

func pgBin(t *testing.T) string {
	ms, _ := filepath.Glob("/usr/lib/postgresql/*/bin/initdb")
	if os.Geteuid() != 0 || os.Getenv("SITATE_DESTRUCTIVE_SETUP") != "1" || len(ms) == 0 {
		t.Skip("root・SITATE_DESTRUCTIVE_SETUP=1・PostgreSQL の道具が要る(使い捨ての容器。受け入れで走らせる)。未検証")
	}
	return filepath.Dir(ms[0])
}

// startPG は使い捨ての PostgreSQL(peer 認証)を起動して試験用のロール・表を作る。
func startPG(t *testing.T, bin, d, root string) (string, string, func(string, string) string, func(string) string) {
	pgd := filepath.Join(d, "pg")
	run := func(s string) {
		t.Helper()
		if out, err := shRoot(t, s, nil); err != nil {
			t.Fatalf("%s\n%s", s, out)
		}
	}
	sup := func(c string) string {
		return fmt.Sprintf("setpriv --reuid=postgres --regid=postgres --init-groups bash -c %s", sq(c))
	}
	run(fmt.Sprintf("mkdir -p /run/postgresql && chown postgres /run/postgresql && install -d -o postgres %s", pgd))
	run(sup(fmt.Sprintf("%s/initdb -D %s --auth-local=peer --auth-host=reject >/dev/null && %s/pg_ctl -D %s -o '-k /run/postgresql -p 5432 -c listen_addresses=' -l %s/log -w start >/dev/null", bin, pgd, bin, pgd, pgd)))
	psql := func(db, sql string) string {
		out, err := shRoot(t, sup(fmt.Sprintf("%s/psql -X -At -h /run/postgresql -d %s -c %s", bin, sq(db), sq(sql))), nil)
		if err != nil {
			t.Fatalf("psql %s: %s", sql, out)
		}
		return out
	}
	psql("postgres", "create role appowner login")
	psql("postgres", "create role other login")
	psql("postgres", "create database app owner appowner")
	psql("app", "create table t1(id int); alter table t1 owner to appowner; create table t2(id int); alter table t2 owner to other; create table rls(id int); alter table rls owner to appowner; alter table rls enable row level security; insert into t2 values (7)")
	psql("app", "select lo_create(0)")
	return bin, pgd, psql, sup
}

func TestSetupAndDBRole(t *testing.T) {
	bin := pgBin(t)
	d := t.TempDir()
	for x := d; x != os.TempDir() && x != "/"; x = filepath.Dir(x) {
		_ = os.Chmod(x, 0o755)
	}
	root := "/srv/sitate-dbtest-data" // 一時ディレクトリの下は根に置けない
	_ = os.RemoveAll(root)
	defer os.RemoveAll(root)
	put(t, filepath.Join(root, "a", "f"), "file", 0o640)
	put(t, filepath.Join(root, "a", "b", "g"), "g", 0o600)
	bin, pgd, psql, sup := startPG(t, bin, d, root)
	defer shRoot(t, sup(fmt.Sprintf("%s/pg_ctl -D %s -m immediate stop", bin, pgd)), nil)
	hba, _ := os.ReadFile(filepath.Join(pgd, "pg_hba.conf"))
	conf, _ := os.ReadFile(filepath.Join(pgd, "postgresql.conf"))

	recvSrc, _ := os.ReadFile("sitate-recv")
	auth := `restrict,from="203.0.113.9",command="/usr/local/libexec/sitate-recv" ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDUMMYDUMMYDUMMYDUMMYDUMMYDUMMYDUMMYDUMMYDUMM`
	targets := fmt.Sprintf("t1\tfiles\t%s\t\"%s\"\nt2\tpostgresql\tapp\t/run/postgresql\t5432\t\"app\"\t\"/run/postgresql\"\t0", root, root)
	recv := strings.Replace(string(recvSrc), "SITATE_TARGETS=''", "SITATE_TARGETS="+sq(targets), 1)
	recv = strings.Replace(recv, "SITATE_AUTH_LINE=''", "SITATE_AUTH_LINE="+sq(auth), 1)
	setup, _ := os.ReadFile("source-setup")
	pre := fmt.Sprintf("SITATE_RECEIVER=%s\nSITATE_PULL_KEY=%s\nSITATE_FROM='203.0.113.9'\nSITATE_TARGETS=%s\nSITATE_CLUSTERS=%s\n",
		sq(recv), sq(auth[strings.Index(auth, "ssh-ed25519"):]), sq(targets), sq("/run/postgresql\t5432\t"+psqlMajor(bin)+"\t"+pgd))
	script := []byte(pre + string(setup))
	steps := func() []map[string]any {
		out, err := shRoot(t, "sh -s", script)
		var l []map[string]any
		for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
			var m map[string]any
			if !strings.HasPrefix(ln, "{") {
				t.Logf("標準エラー等: %s", ln)
				continue
			}
			if json.Unmarshal([]byte(ln), &m) != nil {
				t.Fatalf("JSON でない行 %q\n%s", ln, out)
			}
			l = append(l, m)
		}
		if err != nil {
			t.Fatalf("source-setup 失敗: %s", out)
		}
		return l
	}
	first := steps()
	if last := first[len(first)-1]; last["step"] != "done" || last["result"] == "unchanged" {
		t.Errorf("初回: %v", first)
	}
	for _, s := range steps() { // 再実行では何も変わらない
		if s["result"] != "unchanged" {
			t.Errorf("再実行で変わった段: %v", s)
		}
	}
	checkAfterSetup(t, root, auth, pgd, bin, hba, conf, psql, sup)
}

// checkAfterSetup は source-setup の後の状態(アカウント・ACL・ロールの権限・受け口の操作)を確かめる。
func checkAfterSetup(t *testing.T, root, auth, pgd, bin string, hba, conf []byte, psql func(string, string) string, sup func(string) string) {
	ak, _ := os.ReadFile("/var/lib/sitate-ro/.ssh/authorized_keys")
	if string(ak) != auth+"\n" {
		t.Errorf("authorized_keys: %q", ak)
	}
	asRo := func(c string) (string, error) {
		return shRoot(t, "setpriv --reuid=sitate-ro --regid=sitate-ro --clear-groups sh -c "+sq(c), nil)
	}
	if out, err := asRo("cat " + root + "/a/f " + root + "/a/b/g"); err != nil || out != "fileg" {
		t.Errorf("ACL で読める: %v %q", err, out)
	}
	if _, err := asRo("echo x > " + root + "/a/f"); err == nil {
		t.Errorf("書き込みは拒否される")
	}
	h2, _ := os.ReadFile(filepath.Join(pgd, "pg_hba.conf"))
	c2, _ := os.ReadFile(filepath.Join(pgd, "postgresql.conf"))
	if !bytes.Equal(h2, hba) || !bytes.Equal(c2, conf) {
		t.Errorf("pg_hba.conf・postgresql.conf を変えた")
	}
	recvCall := func(op string) (string, error) {
		return asRo("SSH_ORIGINAL_COMMAND=" + sq(op) + " /usr/local/libexec/sitate-recv")
	}
	if out, err := recvCall("selftest"); err != nil || !strings.Contains(out, `"connect":true`) || !strings.Contains(out, `"unreadable_tables":0`) || !strings.Contains(out, `"large_objects":true`) || !strings.Contains(out, `"match":true`) {
		t.Errorf("selftest: %v %s", err, out)
	}
	if out, err := recvCall("dump-pg t2"); err != nil || !strings.HasPrefix(out, "PGDMP") {
		t.Errorf("別の所有ロールの表・RLS の表を含むダンプ: %v %.20q", err, out)
	}
	if out, err := recvCall("dump-pg-globals t2"); err != nil || !strings.Contains(out, "appowner") || strings.Contains(strings.ToLower(out), "password '") {
		t.Errorf("globals: %v", err)
	}
	if out, _ := shRoot(t, fmt.Sprintf("setpriv --reuid=sitate-ro --regid=sitate-ro --clear-groups %s/psql -X -At -h /run/postgresql -d app -U sitate-ro -c 'select 1 from pg_authid limit 1'", bin), nil); !strings.Contains(out, "permission denied") {
		t.Errorf("pg_authid は読めない: %q", out)
	}
	if out, _ := shRoot(t, "setpriv --reuid=sitate-ro --regid=sitate-ro --clear-groups "+bin+"/psql -X -At -h /run/postgresql -d app -c 'insert into t1 values (1)'", nil); !strings.Contains(out, "permission denied") {
		t.Errorf("書き込みは拒否される: %q", out)
	}
	psql("app", "create role newowner login; create table tnew(id int); alter table tnew owner to newowner")
	if out, err := recvCall("dump-pg t2"); err == nil {
		t.Errorf("導入の後に新しい所有ロールが作った表では dump-pg が非 0: %.30q", out)
	}
	if out, err := recvCall("selftest"); err != nil || strings.Contains(out, `"unreadable_tables":0`) {
		t.Errorf("読めない表の数が出る: %v %s", err, out)
	}
}

func psqlMajor(bin string) string {
	out, _ := exec.Command(bin+"/psql", "--version").Output()
	f := strings.Fields(string(out))
	return strings.Split(f[len(f)-1], ".")[0]
}
