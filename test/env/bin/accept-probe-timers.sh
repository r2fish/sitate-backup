#!/usr/bin/env bash
# 実 systemd の TimeoutStartSec が複数の ExecStart にどう効くか、cron の @reboot がどう動くかの実測(作業指示書 5.1b の申し送り。
# 規則 12: TimeoutStartSec= と ExecStart=- の並び、cron の timeout -k と @reboot)。結果を標準出力に書く。環境は変えない(使い捨ての単位とコンテナだけ)。
# 使い方: accept-probe-timers.sh <NAME>    (accept-env.sh up <NAME> の後)
set -u
name="${1:?NAME}"
b="sitate-acc-$name-backup"
echo "== systemd: $(docker exec "$b" systemctl --version | head -1)"
docker exec -i "$b" bash -s <<'EOS'
set -u
d=/etc/systemd/system
mk() { # mk <unit> <TimeoutStartSec> <ExecStart 行…>
  u="$1"; t="$2"; shift 2
  { printf '[Service]\nType=oneshot\nTimeoutStartSec=%s\n' "$t"; for l in "$@"; do printf 'ExecStart=-%s\n' "$l"; done; } > "$d/$u.service"
}
run() { # run <unit>: 実行して所要時間・結果・各 ExecStart の終了状態を出す
  u="$1"; rm -f /var/tmp/probe.log; systemctl daemon-reload
  SECONDS=0; systemctl start "$u.service" 2>/dev/null; rc=$?
  printf 'unit=%s start_rc=%s elapsed=%ss result=%s\n' "$u" "$rc" "$SECONDS" "$(systemctl show -p Result --value "$u.service")"
  echo "  markers: $(tr '\n' ' ' < /var/tmp/probe.log 2>/dev/null)"
  systemctl show -p ExecMainStatus -p ActiveState --value "$u.service" | tr '\n' ' '; echo
  journalctl -u "$u.service" --no-pager -o cat 2>/dev/null | tail -4 | sed 's/^/  journal: /'
}
step() { echo "sh -c 'sleep $1; echo $2 >> /var/tmp/probe.log'"; }
# (A) 3 つの ExecStart が各 2 秒、TimeoutStartSec=5。合計 6 秒 > 5 秒。命令ごとに数えるなら 3 つとも終わる。全体で数えるなら 3 つ目の途中(5 秒)で打ち切られる。
mk probe-a 5 "/bin/sh -c 'sleep 2; echo a1 >> /var/tmp/probe.log'" "/bin/sh -c 'sleep 2; echo a2 >> /var/tmp/probe.log'" "/bin/sh -c 'sleep 2; echo a3 >> /var/tmp/probe.log'"
echo "(A) 2s×3 本の ExecStart=-、TimeoutStartSec=5"; run probe-a
# (B) 1 つ目が TimeoutStartSec(4 秒)を超えて止まらない。2 つ目が走るか。
mk probe-b 4 "/bin/sh -c 'sleep 30; echo b1 >> /var/tmp/probe.log'" "/bin/sh -c 'echo b2 >> /var/tmp/probe.log'"
echo "(B) 1 本目が 30 秒(TimeoutStartSec=4)、2 本目はすぐ終わる"; run probe-b
# (C) 1 本目が失敗(exit 3)、ExecStart=- なので 2 本目が走るか。
mk probe-c 10 "/bin/sh -c 'echo c1 >> /var/tmp/probe.log; exit 3'" "/bin/sh -c 'echo c2 >> /var/tmp/probe.log'"
echo "(C) 1 本目が exit 3、2 本目は続くか"; run probe-c
# (D) 2 秒 + 4 秒、TimeoutStartSec=5(後ろの 1 本だけが 5 秒を超えないが、合計は超える)
mk probe-d 5 "/bin/sh -c 'sleep 2; echo d1 >> /var/tmp/probe.log'" "/bin/sh -c 'sleep 4; echo d2 >> /var/tmp/probe.log'"
echo "(D) 2s + 4s、TimeoutStartSec=5"; run probe-d
rm -f $d/probe-*.service; systemctl daemon-reload; rm -f /var/tmp/probe.log
EOS
echo
echo "== cron: @reboot と timeout -k(使い捨てのコンテナ。systemd なし)"
img="sitate-acc-backup:local"
cn="sitate-acc-$name-cronprobe"
docker rm -f "$cn" >/dev/null 2>&1
docker run -d --name "$cn" --entrypoint sleep "$img" 3600 >/dev/null
docker exec "$cn" bash -c 'apt-get update -qq >/dev/null 2>&1 && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends cron >/dev/null 2>&1; command -v cron; useradd -m probeusr'
docker exec "$cn" bash -c '
printf "SHELL=/bin/sh\nPATH=/usr/sbin:/usr/bin:/sbin:/bin\nCRON_TZ=Asia/Tokyo\n@reboot probeusr /bin/sh -c \"echo reboot-fired >> /var/tmp/reboot.log\"\n* * * * * probeusr timeout -k 2 3 /bin/sh -c \"echo start >> /var/tmp/cron-timeout.log; sleep 30; echo NOT-REACHED >> /var/tmp/cron-timeout.log\"\n" > /etc/cron.d/sitate-probe
chmod 644 /etc/cron.d/sitate-probe; touch /var/tmp/reboot.log /var/tmp/cron-timeout.log; chmod 666 /var/tmp/*.log
(cron -f >/dev/null 2>&1 &) ; sleep 3
echo "cron 起動 3 秒後の @reboot の記録:"; cat /var/tmp/reboot.log'
echo "docker restart 後(= cron の再起動 = 再起動に相当):"
docker restart -t 1 "$cn" >/dev/null
docker exec "$cn" bash -c '(cron -f >/dev/null 2>&1 &); sleep 3; echo "行数(@reboot が再び走れば 4 行になる):"; wc -l < /var/tmp/reboot.log; echo "cron を再起動しただけ(同じコンテナ内の SIGTERM→起動)でも走るか:"; pkill cron; sleep 1; (cron -f >/dev/null 2>&1 &); sleep 3; wc -l < /var/tmp/reboot.log; ls -l /var/run/crond.reboot; echo "/var/run/crond.reboot を消して(本物の再起動は /run を消す)cron を再起動:"; pkill cron; sleep 1; rm -f /var/run/crond.reboot; (cron -f >/dev/null 2>&1 &); sleep 3; wc -l < /var/tmp/reboot.log'
echo "timeout -k の確認(毎分の行が 1 分以内に start して NOT-REACHED が出ない):"
docker exec "$cn" bash -c 'sleep 62; cat /var/tmp/cron-timeout.log | sort | uniq -c'
docker rm -f "$cn" >/dev/null 2>&1
