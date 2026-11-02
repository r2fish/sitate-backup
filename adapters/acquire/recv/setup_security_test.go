package recv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// source-setup の安全性の試験(レビューの H1〜H9)。root の使い捨て容器で、SITATE_DESTRUCTIVE_SETUP=1 のときだけ流す
// (sitate-ro のアカウントなどを作るため)。受け入れでは `docker run -u 0 -e SITATE_DESTRUCTIVE_SETUP=1 ...` で走らせる。

func destructive(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 || os.Getenv("SITATE_DESTRUCTIVE_SETUP") != "1" {
		t.Skip("root で SITATE_DESTRUCTIVE_SETUP=1 の使い捨て容器だけ。受け入れで走らせる。未検証")
	}
}

const testKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDUMMYDUMMYDUMMYDUMMYDUMMYDUMMYDUMMYDUMMYDUMM"

// runSetupVars は source-setup を `sh -s` で流す。vars は台本の先頭の代入(値は引用する)。
func runSetupVars(t *testing.T, vars map[string]string) ([]map[string]any, string, error) {
	t.Helper()
	body, _ := os.ReadFile("source-setup")
	var pre strings.Builder
	for k, v := range vars {
		pre.WriteString(k + "=" + sq(v) + "\n")
	}
	out, err := shRoot(t, "sh -s", []byte(pre.String()+string(body)))
	var l []map[string]any
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		var m map[string]any
		if strings.HasPrefix(ln, "{") && json.Unmarshal([]byte(ln), &m) == nil {
			l = append(l, m)
		}
	}
	return l, out, err
}

func lastStep(l []map[string]any) map[string]any {
	if len(l) == 0 {
		return map[string]any{}
	}
	return l[len(l)-1]
}

// filesVars は ファイルの対象 t1(根 root)だけの台本の変数。from は空にもできる。
func filesVars(t *testing.T, root, from string) map[string]string {
	rec := fmt.Sprintf("t1\tfiles\t%s\t\"%s\"", root, root)
	rb, _ := os.ReadFile("sitate-recv")
	recv := strings.Replace(string(rb), "SITATE_TARGETS=''", "SITATE_TARGETS="+sq(rec), 1)
	return map[string]string{"SITATE_RECEIVER": recv, "SITATE_PULL_KEY": testKey, "SITATE_FROM": from, "SITATE_TARGETS": rec}
}

func asRoRecv(t *testing.T, op string) (string, error) {
	return shRoot(t, "setpriv --reuid=sitate-ro --regid=sitate-ro --clear-groups env -i SSH_ORIGINAL_COMMAND="+sq(op)+" /usr/local/libexec/sitate-recv", nil)
}

// H9: from が空・不正なら成功にしない。H8: 根がリンクなら実パスも固定の一覧で検査し、/etc/ssh に ACL を付けない。
// H6: 祖先が 0700 でも sitate-ro が根まで届き、再実行は unchanged。
func TestSetupFromAndForbiddenAndAncestor(t *testing.T) {
	destructive(t)
	for _, bad := range []string{"", `a"b`, "a b"} {
		l, out, err := runSetupVars(t, filesVars(t, "/srv/none", bad))
		if err == nil || !strings.Contains(out, `"reason":"from_missing"`) {
			t.Errorf("from=%q: 失敗するはず: %s %v", bad, out, l)
		}
	}
	// H8
	_ = os.MkdirAll("/etc/ssh", 0o755)
	put(t, "/etc/ssh/ssh_host_test_key", "KEY", 0o600)
	_ = os.Remove("/srv/sitate-link")
	if err := os.Symlink("/etc/ssh", "/srv/sitate-link"); err != nil {
		t.Fatal(err)
	}
	defer os.Remove("/srv/sitate-link")
	_, out, err := runSetupVars(t, filesVars(t, "/srv/sitate-link", "203.0.113.9"))
	if err == nil || !strings.Contains(out, `"reason":"root_forbidden"`) {
		t.Errorf("リンクの先が /etc/ssh の根は root_forbidden: %s", out)
	}
	if o, _ := shRoot(t, "getfacl -p /etc/ssh/ssh_host_test_key | grep sitate-ro", nil); strings.TrimSpace(o) != "" {
		t.Errorf("/etc/ssh に ACL が付いた: %s", o)
	}
	// H6
	base := "/srv/sitate-a"
	put(t, base+"/data/f", "F", 0o644)
	_ = os.Chmod(base, 0o700)
	defer os.RemoveAll(base)
	v := filesVars(t, base+"/data", "203.0.113.9")
	if _, out, err := runSetupVars(t, v); err != nil {
		t.Fatalf("0700 の祖先: %s", out)
	}
	if out, err := asRoRecv(t, "list t1"); err != nil || !strings.Contains(out, "f\x00") {
		t.Errorf("0700 の祖先の下でも sitate-ro が一覧できる: %v %q", err, out)
	}
	l, _, err := runSetupVars(t, v)
	for _, s := range l {
		if s["result"] != "unchanged" {
			t.Errorf("再実行で変わった: %v", s)
		}
	}
	if err != nil {
		t.Errorf("再実行: %v", err)
	}
}

// secMk は許可リスト(ファイルの対象 t1 と DB の対象 t2)を埋め込んだ台本の変数を作る関数を返す。
func secMk(bin, pgd, ts, amb string) func(db string, extra ...string) map[string]string {
	return func(db string, extra ...string) map[string]string {
		rec := fmt.Sprintf("t1\tfiles\t%s\t\"%s\"\nt2\tpostgresql\t%s\t/run/postgresql\t5432\t\"x\"\t\"x\"\t0", amb, amb, db)
		rb, _ := os.ReadFile("sitate-recv")
		recv := strings.Replace(string(rb), "SITATE_TARGETS=''", "SITATE_TARGETS="+sq(rec), 1)
		cl := "/run/postgresql\t5432\t" + psqlMajor(bin) + "\t" + pgd + "\t" + ts
		for _, e := range extra {
			cl += "\n" + e
		}
		return map[string]string{"SITATE_RECEIVER": recv, "SITATE_PULL_KEY": testKey, "SITATE_FROM": "203.0.113.9", "SITATE_TARGETS": rec, "SITATE_CLUSTERS": cl}
	}
}

// checkHostileDBNames は DB 名から任意の SQL が走らないことを確かめる(H1)。
func checkHostileDBNames(t *testing.T, mk func(string, ...string) map[string]string, psql func(string, string) string) {
	t.Helper()
	for _, hostile := range []string{"x'; create role pwned superuser; select '", "x$sitate$; create role pwned superuser; select $sitate$y"} {
		_, _, _ = runSetupVars(t, mk(hostile))
		if o := psql("postgres", "select count(*) from pg_roles where rolname = 'pwned'"); strings.TrimSpace(o) != "0" {
			t.Errorf("DB 名から任意の SQL が走った(%q): %s", hostile, o)
		}
	}
}

// H1・H2・H4・H5・M7: 悪意ある DB 名、=を含む DB 名、/usr/bin の道具が無い環境、テーブルスペースの下の根、クラスタの選び方。
func TestSetupDBNamesToolsTablespace(t *testing.T) {
	destructive(t)
	bin := pgBin(t)
	d := t.TempDir()
	for x := d; x != os.TempDir() && x != "/"; x = filepath.Dir(x) {
		_ = os.Chmod(x, 0o755)
	}
	_, pgd, psql, sup := startPG(t, bin, d, d)
	defer shRoot(t, sup(fmt.Sprintf("%s/pg_ctl -D %s -m immediate stop", bin, pgd)), nil)
	weird := "w=1 host=/nonexistent"
	psql("postgres", fmt.Sprintf(`create database "%s" owner appowner`, weird))
	psql("dbname='"+weird+"'", "create table tw(id int); insert into tw values (1)")
	ts := "/srv/sitate-ts1"
	defer os.RemoveAll(ts)
	if _, err := shRoot(t, "rm -rf "+ts+" && install -d -o postgres "+ts, nil); err != nil {
		t.Fatal(err)
	}
	psql("postgres", "create tablespace sitate_ts location '"+ts+"'")
	if out, _ := runSurvey(t, "db_only", "30", t.TempDir()); !strings.Contains(string(out), `"`+ts+`"`) {
		t.Errorf("テーブルスペースの先が raw_paths に入っていない: %s", out)
	}
	amb := "/srv/sitate-amb"
	_ = os.MkdirAll(amb, 0o755)
	defer os.RemoveAll(amb)
	mk := secMk(bin, pgd, ts, amb)
	checkHostileDBNames(t, mk, psql)
	// M7: port の指定が無く、信じるクラスタが 2 つ → cluster_ambiguous
	_, out, err := runSetupVars(t, mk("app", "/run/postgresql\t5499\t"+psqlMajor(bin)+"\t/nonexistent-raw"))
	if err == nil || !strings.Contains(out, `"reason":"cluster_ambiguous"`) {
		t.Errorf("クラスタが 2 つで port の指定なし: %s", out)
	}
	// H5: テーブルスペースの下の根は root_forbidden
	v := mk("app")
	v["SITATE_TARGETS"] = fmt.Sprintf("t1\tfiles\t%s/sub\t\"%s/sub\"", ts, ts)
	_ = os.MkdirAll(ts+"/sub", 0o755)
	if _, out, err := runSetupVars(t, v); err == nil || !strings.Contains(out, `"reason":"root_forbidden"`) {
		t.Errorf("テーブルスペースの下の根: %s", out)
	}
	// 正常な導入(H4: /usr/bin の道具を外す。H2: = を含む DB 名)
	for _, n := range []string{"psql", "pg_dump", "pg_dumpall"} {
		if _, err := shRoot(t, "mv /usr/bin/"+n+" /usr/bin/"+n+".hidden", nil); err != nil {
			t.Fatal(err)
		}
		defer shRoot(t, "mv /usr/bin/"+n+".hidden /usr/bin/"+n, nil)
	}
	rec := fmt.Sprintf("t2\tpostgresql\t%s\t/run/postgresql\t5432\t\"%s\"\t\"/run/postgresql\"\t1", weird, weird)
	v = mk("app")
	v["SITATE_TARGETS"] = rec
	rb, _ := os.ReadFile("sitate-recv")
	v["SITATE_RECEIVER"] = strings.Replace(string(rb), "SITATE_TARGETS=''", "SITATE_TARGETS="+sq(rec), 1)
	if _, out, err := runSetupVars(t, v); err != nil {
		t.Fatalf("/usr/bin に道具が無い環境の導入: %s", out)
	}
	if out, err := asRoRecv(t, "dump-pg t2"); err != nil || !strings.HasPrefix(out, "PGDMP") {
		t.Errorf("= と host= を含む DB 名のダンプ: %v %.30q", err, out)
	}
	if out, err := asRoRecv(t, "selftest"); err != nil || !strings.Contains(out, `"connect":true`) {
		t.Errorf("selftest(/usr/bin の道具なし): %v %s", err, out)
	}
}
