#!/usr/bin/env bash
# A6 (16): 元サーバーで DB の読み取り専用ロール sitate-ro として pg_authid を読む(仕様書 6.5 A6 の (16)。D-0094 の 14)。鍵は要らない。
# 使い方: bash test/acceptance/scripts/A6/check-16.sh <NAME>   (accept-env.sh up <NAME> を済ませたもの。bash 3.2 互換)
# sitate-ro が PostgreSQL に無いとき(A2 の導入がまだ DB の対象を入れていない環境)は、source-setup(adapters/acquire/recv/source-setup)を
# 受け口の試験(setup_root_test.go)と同じ変数の埋め込みで実物のまま 1 回流して作る。作ったかどうかを結果に書く。
# 出力: 1 行 1 項目の「項目 TAB 判定 TAB 注」。終了コード 0 = 合格、1 = 不合格、2 = 前提が無い。
set -u
name="${1:?NAME}"
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../../../.." && pwd)"
c="sitate-acc-$name-source"
docker inspect "$c" >/dev/null 2>&1 || { echo "error: コンテナ $c が無い(accept-env.sh up $name)" >&2; exit 2; }
sock=/var/run/postgresql
port=5432
db=shopdb
pgd=/var/lib/postgresql/17/main
rolesql="select count(*) from pg_roles where rolname='sitate-ro'"
as_pg() { docker exec "$c" bash -c "cd / && runuser -u postgres -- psql -X -At -p $port -d postgres -c \"$1\""; }
ro_psql() { docker exec "$c" bash -c "cd / && runuser -u sitate-ro -- psql -X -w -At -h $sock -p $port -d $db -c \"$1\" 2>&1"; }
sq() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }

created=no
if [ "$(as_pg "$rolesql")" != 1 ]; then
  payload="${TMPDIR:-/tmp}/check16-payload.$$"
  python3 - "$root" "$sock" "$port" "$db" "$pgd" > "$payload" <<'PY'
import sys
root, sock, port, db, pgd = sys.argv[1:6]
sq = lambda x: "'" + x.replace("'", "'\\''") + "'"
key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDUMMYDUMMYDUMMYDUMMYDUMMYDUMMYDUMMYDUMMYDUMM"
targets = "t1\tfiles\t/srv/shopapp\t\"/srv/shopapp\"\nt2\tpostgresql\t%s\t%s\t%s\t\"%s\"\t\"%s\"\t0" % (db, sock, port, db, sock)
recv = open(root + "/adapters/acquire/recv/sitate-recv", encoding="utf-8").read().replace("SITATE_TARGETS=''", "SITATE_TARGETS=" + sq(targets), 1)
clusters = "%s\t%s\t17\t%s" % (sock, port, pgd)
sys.stdout.write("SITATE_RECEIVER=%s\nSITATE_PULL_KEY=%s\nSITATE_FROM='203.0.113.9'\nSITATE_TARGETS=%s\nSITATE_CLUSTERS=%s\n" % (sq(recv), sq(key), sq(targets), sq(clusters)))
sys.stdout.write(open(root + "/adapters/acquire/recv/source-setup", encoding="utf-8").read())
PY
  docker exec -i "$c" sh -s < "$payload" > "$payload.out" 2>&1
  rc=$?
  last="$(grep '^{' "$payload.out" | tail -1)"
  rm -f "$payload" "$payload.out"
  [ "$rc" = 0 ] || { printf 'A6-16\tFAIL\tsource-setup が失敗した(rc=%s, 最後の行 %s)\n' "$rc" "$last"; exit 1; }
  created=yes
fi

fail=0
out1="$(ro_psql 'select 1')"
if [ "$out1" = 1 ]; then printf 'A6-16/前提\tPASS\tsitate-ro で select 1 が通る(ロールを作った=%s)\n' "$created"
else printf 'A6-16/前提\tFAIL\tsitate-ro で接続できない: %s\n' "$out1"; fail=1; fi
attr="$(ro_psql "select rolbypassrls and not rolsuper and rolcanlogin from pg_roles where rolname=current_user")"
[ "$attr" = t ] && printf 'A6-16/属性\tPASS\tBYPASSRLS・非スーパーユーザー・ログイン可\n' || { printf 'A6-16/属性\tFAIL\t%s\n' "$attr"; fail=1; }
out2="$(ro_psql 'select count(*) from pg_authid')"
if printf '%s' "$out2" | grep -q 'permission denied for table pg_authid'; then
  printf 'A6-16\tPASS\tselect from pg_authid は権限のエラー(permission denied for table pg_authid)\n'
else
  printf 'A6-16\tFAIL\tpg_authid が読めた、または別のエラー: %s\n' "$(printf '%s' "$out2" | head -c 200)"; fail=1
fi
exit "$fail"
