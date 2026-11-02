#!/usr/bin/env bash
# 受入試験の環境(source・backup・dest・hook。作業指示書 3.3、5.1b)の起動・停止・環境検査。起動は監督(利用者の端末)。
# 使い方:
#   accept-env.sh up <NAME> [--variant base|add|lo|nocluster] [--scale N]
#       起動。variant は A2 の元サーバーの変種(add: 後で add-extra を実行する回、lo: ラージオブジェクトのある DB、
#       nocluster: DB の対象を持たない 2 つ目のクラスタ(ポート 5433))。scale は合成データの倍率(既定 1)。
#   accept-env.sh down <NAME>            停止(コンテナ・ネットワーク・test/env/.run/accept-<NAME>/ を消す)
#   accept-env.sh status <NAME>          コンテナの状態と接続情報の場所
#   accept-env.sh check <NAME> [--fast] [--exercise-add]   環境検査(完了条件 1〜7・9・11 の前半。--fast は入れ子の rootless 容器の試験を飛ばす)
#   accept-env.sh add-extra <NAME> mount|db   変種 add の「手順 4 の調査の後で手順 8 の前に足す」手順
#   accept-env.sh measure-du <NAME>           SURVEY_DU_TIMEOUT_S の確かめ(元サーバーの /home・/srv の直下を調査と同じ数え方(du -x)で測り、定数と並べる。データ量は up --scale で変える)
#   accept-env.sh exec <NAME> <source|backup|dest|hook> <コマンド…>   コンテナの中で実行(root)
#   accept-env.sh fake-up|fake-down <NAME>    偽の保管先のサーバーを同じ共有の値で起動・停止(完了条件 9 の前半。イテレーション 1 の受入試験は使わない)
#   accept-env.sh probe-timers <NAME>         systemd の TimeoutStartSec と cron の @reboot の実測(申し送り)
# 生成物はすべて test/env/.run/accept-<NAME>/(git 管理外)。資格情報・鍵は起動のたびに生成し、リポジトリには置かない。
# 接続情報の env ファイル: test/env/.run/accept-<NAME>/env(make shell-linux ACCEPT=<NAME> で開発用コンテナに渡る)。
set -eu
op="${1:?up|down|status|check|add-extra|measure-du|exec|fake-up|fake-down|probe-timers}"; name="${2:?NAME}"; shift 2
case "$name" in *[!a-z0-9-]*|"") echo "NAME は小文字・数字・ハイフンだけ" >&2; exit 64;; esac
root="$(cd "$(dirname "$0")/../../.." && pwd)"
acc="$root/test/env/accept"
run="$root/test/env/.run/accept-$name"
export ACC_NAME="$name" ACC_RUN="$run" ACC_ROOT="$root"
export BASE_IMAGE="${BASE_IMAGE:-debian@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587}"
dev_image="${DEV_IMAGE:-sitate-dev:local}"
roles=(source backup dest hook)
cname() { echo "sitate-acc-$name-$1"; }
dc() { docker compose -f "$acc/compose.yaml" -p "sitate-acc-$name" "$@"; }
rnd() { LC_ALL=C tr -dc 'a-z0-9' </dev/urandom | head -c "$1"; }
simtime_write() { # simtime_write <sim_now> <real_at>
  printf '{"format":"sitate.simtime/v1","sim_now":"%s","real_at":"%s"}\n' "$1" "$2" > "$run/shared/simtime.tmp"
  chmod 0666 "$run/shared/simtime.tmp"; mv "$run/shared/simtime.tmp" "$run/shared/simtime"
}
ensure_network_test() { docker network inspect sitate-test >/dev/null 2>&1 || docker network create sitate-test >/dev/null; }

do_down() {
  dc down -v --remove-orphans >/dev/null 2>&1 || true
  docker rm -f "$(cname fakes3)" >/dev/null 2>&1 || true
  # root が作った生成物は消せないことがあるので、消せなければ容器の中から消す。
  rm -rf "$run" 2>/dev/null || { docker run --rm -v "$run:/x" --entrypoint sh "$BASE_IMAGE" -c 'rm -rf /x/* /x/.[!.]*' >/dev/null 2>&1; rm -rf "$run"; }
}

do_up() {
  local variant=base scale=1
  while [ $# -gt 0 ]; do
    case "$1" in
      --variant) variant="${2:?}"; shift 2;;
      --scale) scale="${2:?}"; shift 2;;
      *) echo "引数が不明: $1" >&2; exit 64;;
    esac
  done
  case "$variant" in base|add|lo|nocluster) ;; *) echo "variant は base|add|lo|nocluster" >&2; exit 64;; esac
  case "$scale" in ''|*[!0-9]*) echo "scale は正の整数" >&2; exit 64;; esac
  do_down
  ensure_network_test
  mkdir -p "$run"/{shared,hook-data,certs,ssh,expect} "$root/dist"
  chmod 0777 "$run/shared" "$run/hook-data"
  local t; t="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; simtime_write "$t" "$t"
  # 使い捨ての SSH の鍵とユーザー名(起動のたびに生成)
  local admin="adm$(rnd 5)"
  ssh-keygen -q -t ed25519 -N "" -C "sitate-test-admin" -f "$run/ssh/admin_ed25519"
  # 受け手の証明書(起動時に生成。CA は各ホストへ届ける)
  dc build >/dev/null
  docker run --rm -v "$run/certs:/out" --entrypoint /usr/local/bin/gen-certs sitate-acc-hook:local /out
  dc create >/dev/null
  local r
  for r in source backup dest; do
    local d="$run/hostin-$r"; mkdir -p "$d"
    echo "$r" > "$d/role"; echo "$admin" > "$d/admin_user"
    cp "$run/ssh/admin_ed25519.pub" "$d/admin.pub"
    ssh-keygen -q -t ed25519 -N "" -C "host-$r" -f "$d/ssh_host_ed25519_key"
    ssh-keygen -lf "$d/ssh_host_ed25519_key.pub" | awk '{print $2}' > "$run/ssh/hostkey-$r.fp"
    : > "$d/hostenv"
    if [ "$r" != source ]; then
      cp "$run/certs/ca.pem" "$d/ca.pem"
      printf 'SITATE_EXTRA_CA_FILE=/opt/sitate-test/ca.pem\nSITATE_SIM_TIME_FILE=/opt/sitate-test/shared/simtime\n' > "$d/hostenv"
    fi
    docker cp "$d/." "$(cname "$r"):/opt/sitate-test/in/"
    rm -rf "$d"
  done
  dc start >/dev/null
  # 起動待ち
  for r in source backup dest; do
    local ok=0 i
    for i in $(seq 1 90); do
      if docker exec "$(cname $r)" systemctl is-active ssh >/dev/null 2>&1; then ok=1; break; fi
      sleep 1
    done
    [ "$ok" = 1 ] || { echo "$r の sshd が起動しない" >&2; docker logs "$(cname $r)" 2>&1 | tail -5 >&2; exit 1; }
  done
  for i in $(seq 1 60); do curl -fsS --cacert "$run/certs/ca.pem" --resolve hook:8443:127.0.0.1 https://hook:8443/healthz >/dev/null 2>&1 && break; docker exec "$(cname hook)" python3 -c "import ssl,urllib.request;print(urllib.request.urlopen('https://localhost:8443/healthz',context=ssl.create_default_context(cafile='/etc/hook/tls/server.pem')).status)" >/dev/null 2>&1 && break; sleep 1; done
  # 合成データ
  docker exec "$(cname source)" sitate-gen-source-data "$variant" "$scale" > "$run/expect/source.json" 2> "$run/expect/gen.log" \
    || { echo "合成データの生成に失敗" >&2; tail -5 "$run/expect/gen.log" >&2; exit 1; }
  # 接続情報(資格情報は秘密の値をここにだけ置く。git 管理外)
  cat > "$run/env" <<EOF
SITATE_ACCEPT_NAME=$name
SITATE_ACCEPT_VARIANT=$variant
SITATE_ACCEPT_NETWORK=sitate-acc-$name
SITATE_ACCEPT_SOURCE_HOST=source
SITATE_ACCEPT_SOURCE_PORT=22
SITATE_ACCEPT_BACKUP_HOST=backup
SITATE_ACCEPT_BACKUP_PORT=22
SITATE_ACCEPT_DEST_HOST=dest
SITATE_ACCEPT_DEST_PORT=22
SITATE_ACCEPT_ADMIN_USER=$admin
SITATE_ACCEPT_ADMIN_KEY_FILE=$run/ssh/admin_ed25519
SITATE_ACCEPT_SOURCE_HOST_KEY_FP=$(cat "$run/ssh/hostkey-source.fp")
SITATE_ACCEPT_BACKUP_HOST_KEY_FP=$(cat "$run/ssh/hostkey-backup.fp")
SITATE_ACCEPT_DEST_HOST_KEY_FP=$(cat "$run/ssh/hostkey-dest.fp")
SITATE_ACCEPT_HOOK_URL=https://hook:8443
SITATE_ACCEPT_HOOK_RECORDS_FILE=$run/hook-data/records.jsonl
SITATE_ACCEPT_EXPECT_FILE=$run/expect/source.json
SITATE_ACCEPT_DIST_DIR=$root/dist
SITATE_SIM_TIME_FILE=$run/shared/simtime
SITATE_EXTRA_CA_FILE=$run/certs/ca.pem
EOF
  chmod 0600 "$run/env"
  echo "起動: $name variant=$variant scale=$scale"
  echo "  接続情報: $run/env   期待値(試験の外): $run/expect/source.json"
}

in_dev() { # in_dev <コマンド>: 開発用コンテナで受入試験の環境のネットワークに入って実行
  ACCEPT="$name" bash "$root/test/env/bin/in-linux.sh" "$1"
}

pass=0; failn=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
ng() { failn=$((failn + 1)); echo "FAIL $1"; }
chk() { local label="$1"; shift; if "$@" >/dev/null 2>&1; then ok "$label"; else ng "$label"; fi; }
sx() { docker exec "$(cname "$1")" bash -c "$2"; }

do_check() {
  set +e
  local fast=0 exadd=0
  for a in "$@"; do case "$a" in --fast) fast=1;; --exercise-add) exadd=1;; esac; done
  [ -f "$run/env" ] || { echo "起動していません: accept-env.sh up $name" >&2; exit 64; }
  set -a; . "$run/env"; set +a
  local ak="$SITATE_ACCEPT_ADMIN_KEY_FILE" u="$SITATE_ACCEPT_ADMIN_USER"
  echo "# 11: 役割名・ホスト名"
  for r in source backup dest hook; do
    chk "コンテナ $r のホスト名が $r" test "$(docker exec "$(cname $r)" hostname)" = "$r"
  done
  echo "# 1: 開発用コンテナで rootless の容器"
  if [ "$fast" = 1 ]; then echo "skip (--fast)"; else
    chk "rootless の podman が alpine を起動する" bash -c "SITATE_NESTED=1 bash '$root/test/env/bin/in-linux.sh' 'podman run --rm docker.io/library/alpine:3.20 true'"
  fi
  echo "# 2: SSH(開発用コンテナから各ホスト。バックアップサーバーから元サーバー・復元先)"
  local sshopt="-i $ak -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/tmp/kh -o BatchMode=yes -o ConnectTimeout=10"
  local r
  for r in source backup dest; do
    chk "開発用コンテナ → $r に管理用鍵で入れる" in_dev "ssh $sshopt $u@$r true"
    chk "$r の sshd が 22 で待ち受ける" sx "$r" "ss -ltn | grep -q ':22 '"
  done
  chk "元サーバーのホスト鍵が記録した指紋と一致" bash -c "[ \"\$(docker exec $(cname source) ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub | awk '{print \$2}')\" = '$SITATE_ACCEPT_SOURCE_HOST_KEY_FP' ]"
  # バックアップサーバーから元サーバー・復元先(検査の間だけ管理用鍵を /tmp に置く)
  docker cp "$ak" "$(cname backup):/var/tmp/chk_key" && sx backup "chmod 600 /var/tmp/chk_key"
  for r in source dest; do
    chk "backup → $r に SSH が通る" sx backup "ssh -i /var/tmp/chk_key -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/var/tmp/kh -o BatchMode=yes -o ConnectTimeout=10 $u@$r true"
  done
  sx backup "rm -f /var/tmp/chk_key /var/tmp/kh" || true
  chk "公開ポートが無い(ホストに公開していない)" bash -c "[ -z \"\$(docker ps --filter name=sitate-acc-$name- --format '{{.Ports}}' | tr -d '\n ')\" ]"
  echo "# 3: PostgreSQL と変種"
  chk "元サーバーの PostgreSQL に接続できる" sx source "runuser -u postgres -- psql -X -Atc 'select 1' | grep -q 1"
  chk "pg_hba.conf に local の peer の行(既定)" sx source "grep -Eq '^local[[:space:]]+all[[:space:]]+all[[:space:]]+peer' /etc/postgresql/17/main/pg_hba.conf"
  chk "pg_hba.conf が pg_createcluster の既定のまま(trust・md5・scram の local 行が無い)" sx source "! grep -Eq '^local[[:space:]].*(trust|md5|scram)' /etc/postgresql/17/main/pg_hba.conf"
  chk "段 prereq の道具が揃う(GNU findutils・GNU tar・setfacl・sha256sum・runuser・psql)" sx source "find --version | grep -q 'GNU findutils' && tar --version | grep -q 'GNU tar' && command -v setfacl sha256sum runuser psql"
  chk "ACL が付けられる(/srv の下)" sx source "setfacl -m u:nobody:r /srv/shopapp/config/app.conf && getfacl -p /srv/shopapp/config/app.conf | grep -q 'user:nobody:r--' && setfacl -x u:nobody /srv/shopapp/config/app.conf"
  chk "元サーバーの sshd が 22(Port の設定が無い)" sx source "! grep -Ehr '^[[:space:]]*Port[[:space:]]+[0-9]+' /etc/ssh/sshd_config /etc/ssh/sshd_config.d/ | grep -qv ' 22$'"
  chk "試験環境の都合のマウント(/etc/hosts 等)が元サーバーの mountinfo に出ない" sx source "! awk '{print \$5}' /proc/self/mountinfo | grep -Eq '^/etc/(hosts|hostname|resolv.conf)$'"
  local variant="$SITATE_ACCEPT_VARIANT"
  case "$variant" in
  lo) chk "(b) pg_largeobject_metadata に行がある" sx source "[ \"\$(runuser -u postgres -- psql -X -Atd docstore -c 'select count(*) from pg_largeobject_metadata')\" -gt 0 ]";;
  nocluster) chk "(c) 2 つ目のクラスタ(5433)が動いている" sx source "runuser -u postgres -- pg_isready -p 5433 -q && ls /var/run/postgresql/.s.PGSQL.5433";;
  add)
    chk "(a) 足す手順の道具がある" sx source "command -v sitate-source-add-extra mkfs.ext4"
    if [ "$exadd" = 1 ]; then
      local m0 d0; m0="$(sx source "grep -c ' ext4 ' /proc/self/mountinfo" || true)"; d0="$(sx source "runuser -u postgres -- psql -X -Atc 'select count(*) from pg_database'")"
      do_add_extra mount >/dev/null; do_add_extra db >/dev/null
      chk "(a) マウントが増えた" test "$(sx source "grep -c ' ext4 ' /proc/self/mountinfo")" -gt "$m0"
      chk "(a) DB が増えた" test "$(sx source "runuser -u postgres -- psql -X -Atc 'select count(*) from pg_database'")" -gt "$d0"
    fi;;
  esac
  echo "# 4: バックアップサーバーのタイマー"
  chk "backup は systemd が動く(/run/systemd/system)" sx backup "test -d /run/systemd/system && systemctl is-system-running --wait | grep -Eq 'running|degraded'"
  chk "backup の timers.target が有効" sx backup "systemctl is-active timers.target"
  chk "backup で一時タイマーが実際に起動する(systemd-run --on-active)" sx backup "systemd-run --quiet --on-active=2 --timer-property=AccuracySec=1s --unit=chk-timer touch /var/tmp/chk-timer-fired && sleep 6 && test -f /var/tmp/chk-timer-fired && rm -f /var/tmp/chk-timer-fired"
  chk "backup の /etc/systemd/system に単位を置いて timer を有効にできる(init の Timers の登録と同じ経路)" sx backup "printf '[Service]\nType=oneshot\nExecStart=-/bin/true\n' > /etc/systemd/system/sitate-chk.service && printf '[Timer]\nOnCalendar=daily\nPersistent=true\n[Install]\nWantedBy=timers.target\n' > /etc/systemd/system/sitate-chk.timer && systemctl daemon-reload && systemctl enable --now sitate-chk.timer && systemctl is-enabled sitate-chk.timer && systemctl list-timers sitate-chk.timer --no-pager | grep -q sitate-chk; rc=\$?; systemctl disable --now sitate-chk.timer >/dev/null 2>&1; rm -f /etc/systemd/system/sitate-chk.*; systemctl daemon-reload; exit \$rc"
  chk "backup でユーザーの単位を常駐させられる(loginctl enable-linger。timer_permission)" sx backup "useradd -m chklinger && loginctl enable-linger chklinger && test -f /var/lib/systemd/linger/chklinger; rc=\$?; loginctl disable-linger chklinger; userdel -r chklinger >/dev/null 2>&1; exit \$rc"
  chk "backup は空(sitate ユーザーも podman も無い)" sx backup "! id sitate && ! command -v podman"
  chk "dest でファイアウォールの道具が動く(iptables か nft)" sx dest "iptables -S >/dev/null 2>&1 || nft list ruleset >/dev/null 2>&1"
  chk "dest の /etc/hosts に root が追記できる(egress_allow の固定)" sx dest "test -w /etc/hosts"
  chk "dest の一般ユーザーが gocryptfs を FUSE でマウントできる(使い捨て暗号化領域)" sx dest "useradd -m chkrestore && runuser -u chkrestore -- bash -c 'cd ~ && mkdir c p && echo pw | gocryptfs -init -q c >/dev/null && echo pw | gocryptfs -q c p && echo hello > p/f && fusermount3 -u p && ls c | grep -q gocryptfs.conf'; rc=\$?; userdel -r chkrestore >/dev/null 2>&1; exit \$rc"
  if [ "$fast" = 0 ]; then
    chk "dest の一般ユーザーが rootless の podman で容器を起動できる" sx dest "useradd -m chkpodman && runuser -u chkpodman -- bash -c 'cd ~ && podman run --rm docker.io/library/alpine:3.20 true'; rc=\$?; userdel -r chkpodman >/dev/null 2>&1; exit \$rc"
  fi
  chk "dest と backup に make build-test の成果物の置き場(dist)が読み取り専用で見える" bash -c "docker exec $(cname dest) test -d /opt/sitate-test/dist && docker exec $(cname backup) test -d /opt/sitate-test/dist"
  chk "dest に rootless の podman・gocryptfs・fusermount3・/dev/fuse がある" sx dest "command -v podman gocryptfs fusermount3 && test -c /dev/fuse"
  echo "# 5: Webhook の受け手"
  local H=https://hook:8443 CA=/opt/sitate-test/ca.pem
  chk "自己署名の受け手は既定の信頼では検証に失敗する" sx backup "! curl -fsS $H/healthz"
  chk "追加の CA の環境変数が SSH のセッションに届く" in_dev "ssh $sshopt $u@backup 'test \"\$SITATE_EXTRA_CA_FILE\" = $CA'"
  chk "追加の CA で HTTPS の POST が通る(SSH のセッションの環境変数で)" in_dev "ssh $sshopt $u@backup 'curl -fsS --cacert \"\$SITATE_EXTRA_CA_FILE\" -H \"Content-Type: application/json; charset=utf-8\" -H \"Sitate-Event-Id: chk-1\" -d \"{\\\"type\\\":\\\"notify.test\\\",\\\"facts\\\":{\\\"confirmation_word\\\":\\\"chkword\\\"}}\" $H/hooks/chk'"
  chk "記録ファイルに 1 件 1 行の JSON が追記される" bash -c "[ \"\$(grep -c . '$run/hook-data/records.jsonl')\" -ge 1 ] && python3 -c \"import json;[json.loads(l) for l in open('$run/hook-data/records.jsonl')]\""
  chk "GET の口で宛先ごとに同じ記録が読める(/records/chk)" in_dev "curl -fsS --cacert \$SITATE_EXTRA_CA_FILE --resolve hook:8443:\$(getent hosts hook | cut -d' ' -f1) $H/records/chk | jq -e '.[0].dest==\"chk\" and .[0].body.type==\"notify.test\"'"
  chk "確認語が読み出せる(/confirmation/chk)" in_dev "curl -fsS --cacert \$SITATE_EXTRA_CA_FILE $H/confirmation/chk | jq -e '.confirmation_word==\"chkword\"'"
  chk "別の宛先の記録に混ざらない" in_dev "curl -fsS --cacert \$SITATE_EXTRA_CA_FILE $H/records/other | jq -e 'length==0'"
  echo "# 6・11: 合成データの生成器と期待値の JSON"
  local ex="$SITATE_ACCEPT_EXPECT_FILE"
  chk "期待値の JSON が sitate.accept-expectations/v1" python3 -c "import json,sys;d=json.load(open('$ex'));assert d['format']=='sitate.accept-expectations/v1';assert d['personal_names']['strings'];assert d['databases']"
  chk "個人名入りのファイル名が元サーバーの対象の根の下にある" bash -c "python3 -c \"import json;[print(x['path']) for x in json.load(open('$ex'))['personal_names']['target_file_names']]\" | while IFS= read -r p; do docker exec $(cname source) test -f \"\$p\" || exit 1; done"
  chk "個人名入りのディレクトリ名(対象の根の下)がある" bash -c "python3 -c \"import json;[print(x['path']) for x in json.load(open('$ex'))['personal_names']['target_dir_names']]\" | while IFS= read -r p; do docker exec $(cname source) test -d \"\$p\" || exit 1; done"
  chk "/home・/srv の直下に個人名入りのディレクトリがある" bash -c "python3 -c \"import json;[print(x['parent']+'/'+x['name']) for x in json.load(open('$ex'))['personal_names']['survey_dir_names']]\" | while IFS= read -r p; do docker exec $(cname source) test -d \"\$p\" || exit 1; done"
  chk "個人名入りの DB 名がある" sx source "runuser -u postgres -- psql -X -Atc 'select datname from pg_database' | grep -qx sawatari_ryo_shop"
  chk "DB の表の行数が期待値と一致" bash -c "python3 -c \"
import json,subprocess
d=json.load(open('$ex'))
for db in d['databases']:
    for t,n in db['rows'].items():
        o=subprocess.check_output(['docker','exec','$(cname source)','runuser','-u','postgres','--','psql','-X','-At','-p',str(db['cluster_port']),'-d',db['name'],'-c','select count(*) from '+t]).decode().strip()
        assert int(o)==n,(db['name'],t,o,n)
\""
  echo "# 7: 資格情報がリポジトリに置かれない"
  chk "生成物は git 管理外(.gitignore の test/env/.run/)" bash -c "cd '$root' && git check-ignore -q '$run/env' && git check-ignore -q '$run/ssh/admin_ed25519'"
  chk "リポジトリの追跡ファイルに生成した鍵・ユーザー名・証明書が無い" bash -c "cd '$root' && ! git grep -lI -e '$u' -e 'BEGIN OPENSSH PRIVATE KEY' -e 'BEGIN PRIVATE KEY' -- test/env 2>/dev/null | grep -q ."
  echo "# 9(前半): 共有の値"
  chk "backup で書き換えた共有の値を dest も読む(同じ 1 ファイル)" bash -c "docker exec $(cname backup) bash -c \"printf '%s\\n' '{\\\"format\\\":\\\"sitate.simtime/v1\\\",\\\"sim_now\\\":\\\"2030-01-01T00:00:00Z\\\",\\\"real_at\\\":\\\"2026-11-02T00:00:00Z\\\"}' > /opt/sitate-test/shared/simtime\" && docker exec $(cname dest) grep -q 2030-01-01 /opt/sitate-test/shared/simtime"
  chk "SSH のセッション(強制コマンド)に共有の値の環境変数が届く" in_dev "ssh $sshopt $u@backup 'sudo useradd -m gateprobe 2>/dev/null; sudo install -d -m 700 -o gateprobe /home/gateprobe/.ssh; echo \"command=\\\"printenv SITATE_SIM_TIME_FILE\\\" \$(cat ~/.ssh/authorized_keys)\" | sudo tee /home/gateprobe/.ssh/authorized_keys >/dev/null; sudo chown gateprobe /home/gateprobe/.ssh/authorized_keys'; ssh $sshopt gateprobe@backup x | grep -qx /opt/sitate-test/shared/simtime"
  chk "systemd の単位に共有の値の環境変数が届く(DefaultEnvironment)" sx backup "systemd-run --quiet --wait --pipe printenv SITATE_SIM_TIME_FILE | grep -qx /opt/sitate-test/shared/simtime"
  if docker inspect "$(cname fakes3)" >/dev/null 2>&1; then
    # 署名つきの GET /<bucket>(有効な共有の値なら 501 NotImplemented、共有の値が壊れていれば 500 を返す。署名の検証の後で時計を読むため)
    . "$run/fake/env"
    fcode() { docker exec "$(cname backup)" curl -s -o /dev/null -w '%{http_code}' --aws-sigv4 "aws:amz:us-east-1:s3" --user "$SITATE_ACCEPT_FAKE_S3_ACCESS_KEY_ID:$SITATE_ACCEPT_FAKE_S3_SECRET_ACCESS_KEY" -H "x-amz-content-sha256: UNSIGNED-PAYLOAD" http://fakes3:9000/probe; }
    local t0; t0="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; simtime_write "$t0" "$t0"
    chk "偽の保管先は有効な共有の値で 500 を返さない" test "$(fcode)" != 500
    printf 'broken\n' > "$run/shared/simtime"
    chk "共有の値を壊すと、再起動なしに偽の保管先が 500 を返す" test "$(fcode)" = 500
    simtime_write "$t0" "$t0"
    chk "共有の値を戻すと、再起動なしに 500 が止む" test "$(fcode)" != 500
    local t1; t1="$(date -u -v+400d +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '+400 days' +%Y-%m-%dT%H:%M:%SZ)"; simtime_write "$t1" "$t0"
    chk "模擬時刻を +400 日にしても偽の保管先が動き続ける" test "$(fcode)" != 500
    simtime_write "$t0" "$t0"
  else
    echo "skip 偽の保管先のサーバー未起動(accept-env.sh fake-up $name)"
  fi
  echo "集計: ok=$pass FAIL=$failn"
  [ "$failn" = 0 ]
}

do_add_extra() {
  sx source "sitate-source-add-extra ${1:?mount|db}"
}

do_fake_up() {
  local d="$run/fake"; mkdir -p "$d"
  local ak sk; ak="$(rnd 20)"; sk="$(rnd 40)"
  docker rm -f "$(cname fakes3)" >/dev/null 2>&1 || true
  [ "$(docker image inspect -f '{{index .Config.Labels "sitate.dockerfile"}}' "$dev_image" 2>/dev/null)" ] || bash "$root/test/env/bin/in-linux.sh" true
  docker run -d --name "$(cname fakes3)" --network "sitate-acc-$name" --network-alias fakes3 --user "$(id -u):$(id -g)" -v "$root:$root" -w "$root" -v sitate-dev-cache:/cache \
    -v "$run/shared:/opt/sitate-test/shared" -e SITATE_SIM_TIME_FILE=/opt/sitate-test/shared/simtime -e GOFLAGS=-buildvcs=false -e GOCACHE=/cache/go-build -e GOMODCACHE=/cache/go-mod -e HOME=/cache/home \
    "$dev_image" go run ./internal/contract/fake/cmd/fakes3 -listen :9000 -access-key "$ak" -secret-key "$sk" >/dev/null
  for _ in $(seq 1 120); do docker logs "$(cname fakes3)" 2>&1 | grep -q 'fakes3 listening' && break; sleep 1; done
  docker logs "$(cname fakes3)" 2>&1 | grep -q 'fakes3 listening' || { echo "偽の保管先が起動しない" >&2; exit 1; }
  printf 'SITATE_ACCEPT_FAKE_S3_ENDPOINT=http://fakes3:9000\nSITATE_ACCEPT_FAKE_S3_ACCESS_KEY_ID=%s\nSITATE_ACCEPT_FAKE_S3_SECRET_ACCESS_KEY=%s\n' "$ak" "$sk" > "$d/env"; chmod 0600 "$d/env"
  echo "起動: 偽の保管先 http://fakes3:9000(資格情報: $d/env)"
}

do_measure_du() {
  # 元サーバーの調査が数えるディレクトリ(/home・/srv の直下)を、調査と同じ数え方(ファイルシステムをまたがずに du)で測る。
  local lim; lim="$(grep -E '^[[:space:]]*SurveyDuTimeoutS[[:space:]]*=' "$root/internal/contract/constants.go" | grep -Eo '[0-9]+' | head -1 || true)"
  echo "SURVEY_DU_TIMEOUT_S(internal/contract の値)= ${lim:-不明}"
  docker exec "$(cname source)" bash -c '
    for d in /home/* /srv/*; do
      [ -d "$d" ] || continue
      s=$(date +%s%N); b=$(du -sx -B1 "$d" 2>/dev/null | cut -f1); e=$(date +%s%N)
      printf "%-40s bytes=%-12s files=%-7s du_ms=%s\n" "$d" "$b" "$(find "$d" -xdev 2>/dev/null | wc -l)" $(( (e - s) / 1000000 ))
    done'
}

case "$op" in
  up) do_up "$@";;
  down) do_down; echo "停止: $name";;
  status) dc ps; echo "接続情報: $run/env";;
  check) do_check "$@" 2>&1;;
  add-extra) do_add_extra "$@";;
  measure-du) do_measure_du;;
  exec) r="${1:?role}"; shift; docker exec "$(cname "$r")" "$@";;
  fake-up) do_fake_up;;
  fake-down) docker rm -f "$(cname fakes3)" >/dev/null 2>&1 || true; echo "停止: 偽の保管先";;
  probe-timers) bash "$root/test/env/bin/accept-probe-timers.sh" "$name";;
  *) echo "op が不明" >&2; exit 64;;
esac
