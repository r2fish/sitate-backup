#!/usr/bin/env bash
# A6 (6): 共有の模擬時間(sitate.simtime/v1)を確かめ、進める(作業指示書 3.3 の共有の値。bash 3.2 互換)。
#   simtime.sh <NAME> show                進める前後の確認: 共有の値の sim_now と、backup・dest の `sitate version` の sim_time を並べ、一致を判定する
#   simtime.sh <NAME> advance <日数>      sim_now を <日数> 日進める(real_at は書く時点の実時刻。一時ファイルに書いて mv。accept-env.sh の simtime_write と同じ形)。
#                                         進める前と後に show と同じ確認を行い、前後の差が日数と一致することを確かめる。保管先の時計は進めない。
# dest に核(sitate)は無い環境のため、dest は共有の値のファイルが同じ中身で見えることで確かめる。
# 出力: 1 行 1 項目の「項目 TAB 判定 TAB 注」。終了コード 0 = 合格。
set -u
name="${1:?NAME}"; op="${2:?show|advance}"
root="$(cd "$(dirname "$0")/../../../.." && pwd)"
run="$root/test/env/.run/accept-$name/shared"
[ -f "$run/simtime" ] || { echo "error: $run/simtime が無い(accept-env.sh up $name)" >&2; exit 2; }
ae="bash $root/test/env/bin/accept-env.sh"
shared_now() { python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["sim_now"])' "$run/simtime"; }
host_now() { # host_now <backup|dest>: `sitate version` の sim_time(sitate ユーザーとして、試験ビルドの環境変数つき)
  $ae exec "$name" "$1" bash -c 'cd /; runuser -u sitate -- env SITATE_SIM_TIME_FILE=/opt/sitate-test/shared/simtime SITATE_TEST_TRUST_PUBKEY_FILE=/etc/sitate-test/trust.pub /usr/local/bin/sitate version' \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d["result"]["sim_time"])'
}
epoch() { python3 -c 'import sys,datetime;print(int(datetime.datetime.strptime(sys.argv[1],"%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=datetime.timezone.utc).timestamp()))' "$1"; }
fail=0
check() { # check <ラベル>: backup の `sitate version` の sim_time が、共有の値から期待する時刻(sim_now + 実時間の経過)と 10 秒以内で一致し、dest も同じ共有の値を読めるか
  local b exp dsum hsum
  b="$(host_now backup)"
  exp="$(python3 -c 'import json,sys,datetime as D;d=json.load(open(sys.argv[1]));f="%Y-%m-%dT%H:%M:%SZ";p=lambda x:D.datetime.strptime(x,f).replace(tzinfo=D.timezone.utc);print(int((p(d["sim_now"])+(D.datetime.now(D.timezone.utc)-p(d["real_at"]))).timestamp()))' "$run/simtime")"
  eb="$(epoch "$b")"
  dsum="$($ae exec "$name" dest sha256sum /opt/sitate-test/shared/simtime | cut -d' ' -f1)"
  hsum="$(shasum -a 256 "$run/simtime" | cut -d' ' -f1)"
  if [ $((eb - exp)) -ge -10 ] && [ $((eb - exp)) -le 10 ] && [ "$dsum" = "$hsum" ]; then
    printf 'A6-6/%s\tPASS\t共有 sim_now=%s backup の sitate version sim_time=%s dest は同じ共有の値を読む(sha256 一致)\n' "$1" "$(shared_now)" "$b"
  else printf 'A6-6/%s\tFAIL\t共有 sim_now=%s backup=%s 期待との差=%s 秒 dest の共有の値の一致=%s\n' "$1" "$(shared_now)" "$b" "$((eb - exp))" "$([ "$dsum" = "$hsum" ] && echo yes || echo no)"; fail=1; fi
  SIM_CHECKED="$eb"
}
case "$op" in
  show) check 確認;;
  advance)
    days="${3:?日数}"; case "$days" in ''|*[!0-9]*) echo "日数は正の整数" >&2; exit 64;; esac
    check 進める前; before="$SIM_CHECKED"
    new="$(python3 -c 'import sys,datetime as D;print((D.datetime.fromtimestamp(int(sys.argv[1])+int(sys.argv[2])*86400,D.timezone.utc)).strftime("%Y-%m-%dT%H:%M:%SZ"))' "$before" "$days")"  # 今の模擬時刻(sim_now + 経過)に日数を足す
    now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf '{"format":"sitate.simtime/v1","sim_now":"%s","real_at":"%s"}\n' "$new" "$now" > "$run/simtime.tmp"; chmod 0666 "$run/simtime.tmp"; mv "$run/simtime.tmp" "$run/simtime"
    check 進めた後; after="$SIM_CHECKED"
    delta=$((after - before))
    if [ "$delta" -ge $((days * 86400)) ] && [ "$delta" -le $((days * 86400 + 600)) ]; then printf 'A6-6/進めた量\tPASS\t%s 日(差 %s 秒)\n' "$days" "$delta"
    else printf 'A6-6/進めた量\tFAIL\t%s 日のはずが差 %s 秒\n' "$days" "$delta"; fail=1; fi;;
  *) echo "show|advance" >&2; exit 64;;
esac
exit "$fail"
