package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 実物の PostgreSQL・pg_dump・受け口(sitate-recv)を通す試験。root は要らない: 試験の実行ユーザーを DB のロール(peer 認証)にし、
// 受け口の ROLE をそのユーザー名に置き換える。PostgreSQL の道具が無い環境(作業端末の直)では走らせられない
// (開発用コンテナの中で走る)。root が要る本番どおりの権限の検査(`sitate-ro` の権限・RLS・ラージオブジェクト)は U2a の受け口の試験の範囲。

const pgTestPort = "55432"

func pgBinDir(t *testing.T) string {
	t.Helper()
	ms, _ := filepath.Glob("/usr/lib/postgresql/*/bin/initdb")
	if len(ms) == 0 {
		t.Skip("PostgreSQL の道具が無い(開発用コンテナで走らせる)。未検証")
	}
	if os.Geteuid() == 0 {
		t.Skip("root では initdb を動かせない")
	}
	return filepath.Dir(ms[len(ms)-1])
}

type realPG struct {
	bin, sock string
}

func startRealPG(t *testing.T) realPG {
	bin := pgBinDir(t)
	base, err := os.MkdirTemp("", "sitate-pg-")
	if err != nil {
		t.Fatal(err)
	}
	pgd, sock := filepath.Join(base, "d"), filepath.Join(base, "s")
	if err := os.Mkdir(sock, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command(filepath.Join(bin, "pg_ctl"), "-D", pgd, "-m", "immediate", "stop").Run()
		os.RemoveAll(base)
	})
	sh := func(name string, args ...string) {
		t.Helper()
		if out, err := exec.Command(filepath.Join(bin, name), args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
	}
	sh("initdb", "-D", pgd, "--auth-local=peer", "--auth-host=reject", "-A", "peer")
	sh("pg_ctl", "-D", pgd, "-o", "-k "+sock+" -p "+pgTestPort+" -c listen_addresses=", "-l", filepath.Join(base, "log"), "-w", "start")
	return realPG{bin, sock}
}

func (p realPG) psql(t *testing.T, db, sql string) {
	t.Helper()
	out, err := exec.Command(filepath.Join(p.bin, "psql"), "-X", "-q", "-h", p.sock, "-p", pgTestPort, "-d", db, "-v", "ON_ERROR_STOP=1", "-c", sql).CombinedOutput()
	if err != nil {
		t.Fatalf("psql %q: %v\n%s", sql, err, out)
	}
}

// helper は実物の受け口に許可リスト(t2 = DB app)を埋め込み、`<op> <id>` を SSH_ORIGINAL_COMMAND にして実行する補助コマンドを作る。
func (p realPG) helper(t *testing.T, db string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "recv", "sitate-recv"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	rec := fmt.Sprintf("t2\tpostgresql\t%s\t%s\t%s\t%q\t%q\t1", db, p.sock, pgTestPort, db, p.sock)
	s := strings.Replace(string(src), "SITATE_TARGETS=''", "SITATE_TARGETS="+sq(rec), 1)
	if !strings.Contains(s, "\nROLE=sitate-ro\n") {
		t.Fatal("受け口の ROLE の行が見つからない")
	}
	s = strings.Replace(s, "\nROLE=sitate-ro\n", "\nROLE="+u.Username+"\n", 1)
	dir := t.TempDir()
	script := filepath.Join(dir, "recv.sh")
	if err := os.WriteFile(script, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nSSH_ORIGINAL_COMMAND=\"$1 $2\" exec sh "+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return helper
}

// 実物の pg_dump のカスタム形式と pg_dumpall の globals が dump フレームで届き、ロールのパスワードは含まれない。存在しない DB は項目の失敗になる。
func TestRealPostgreSQL(t *testing.T) {
	pg := startRealPG(t)
	pg.psql(t, "postgres", "CREATE ROLE appowner LOGIN PASSWORD 'secret-pass-for-test'")
	pg.psql(t, "postgres", "CREATE DATABASE app OWNER appowner")
	pg.psql(t, "app", "CREATE TABLE t1(id int); INSERT INTO t1 SELECT generate_series(1, 1000)")

	r := runAdapter(t, pg.helper(t, "app"), request("t2"))
	if r.code != 0 {
		t.Fatalf("終了 %d\n%.300s", r.code, r.stdout)
	}
	p := parseOutput(t, r.stdout)
	d := p.dumps(t)
	if p.result.Status != "ok" || len(d) != 2 || d[0].format != contract.DumpFormatGlobals || d[1].format != contract.DumpFormatCustom {
		t.Fatalf("結果: %s", p.line)
	}
	if !bytes.HasPrefix(d[1].body, []byte("PGDMP")) {
		t.Errorf("custom の本体が pg_dump のカスタム形式でない: %.20q", d[1].body)
	}
	if !bytes.Contains(d[0].body, []byte("CREATE ROLE appowner")) || bytes.Contains(d[0].body, []byte("secret-pass-for-test")) || bytes.Contains(bytes.ToUpper(d[0].body), []byte("PASSWORD")) {
		t.Errorf("globals にロール定義が無いか、パスワードを含む: %.200s", d[0].body)
	}

	// 存在しない DB: pg_dump の接続の失敗が項目の失敗(両項目が失敗なので failed)。DB 名は結果に出ない。
	r = runAdapter(t, pg.helper(t, "nodb-secretname"), request("t2"))
	p = parseOutput(t, r.stdout)
	if r.code != 0 || p.result.Status != "failed" || strings.Contains(string(r.stdout), "secretname") {
		t.Errorf("code=%d 結果: %s", r.code, p.line)
	}
}
