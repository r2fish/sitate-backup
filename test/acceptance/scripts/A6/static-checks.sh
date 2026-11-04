#!/usr/bin/env bash
# A6 の静的検査(鍵も受入試験の環境も要らない)。仕様書 6.5 A6 の (15)(17)(18) の静的な部分を、アプリの既存の試験で項目 ID ごとに実行して集計行を出す。
#   A6-15: エージェントのプロセスの依存の木に agent-toolset・memory・child_process の import が無いこと(静的 + 実行時に評価されないこと + 道具の一覧)
#   A6-17: 最上位の画面が 5 面だけで、開く窓が本体の 1 つと OS の標準の窓だけであること
#   A6-15a: エージェントのプロセスの依存の木と束ねた成果物で、fetch・WebSocket・XMLHttpRequest と http・https・net・tls・dgram・undici・ws の経路が guarded fetch の外に無い(静的。D-0177)
#   A6-15b/c: エージェントのプロセスを製品と同じ実行引数で実際に起動し、口の外の経路が使われない・禁じた経路が失敗する(b)、別のホストへの送信が guarded fetch で失敗し記録に残る(c)。
#             プロセスの記録(JSON Lines)は環境変数 A6NET_RECORD_FILE の指すファイルに書く(check_a6.py --net-record が読む)。偽の LLM の口は 127.0.0.1 の試験用で、実物の LLM API へは送らない
#   A6-N1: 通知のイベントの facts(契約のスキーマの全種類)にファイル名・パス・自由文の欄が無く、アプリの文はカタログの固定文だけ(通知本文を注入する口が無いことの静的な確かめ。D-0177)
#   A6-18: sitate_attest の step に署名を要さない 3 段を渡すと TOOL_ARG_INVALID(静的: 定義の列挙に 3 段が無い。試行: ゲートが拒む)
# 使い方: bash test/acceptance/scripts/A6/static-checks.sh   (npm --prefix app ci の後。bash 3.2 互換)
# 出力: 項目 TAB 判定 TAB ノードの集計行(pass/fail)。終了コード 0 = 全項目合格。
set -u
cd "$(dirname "$0")/../../../../app" || exit 2
rc=0
run() { # run <項目> <ファイル…>
  local item="$1"; shift
  local out; out="$(node --test --test-timeout=60000 "$@" 2>&1)"
  local p f
  p="$(printf '%s\n' "$out" | sed -n 's/^ℹ pass \([0-9]*\)$/\1/p')"
  f="$(printf '%s\n' "$out" | sed -n 's/^ℹ fail \([0-9]*\)$/\1/p')"
  if [ "${f:-x}" = 0 ] && [ "${p:-0}" -gt 0 ]; then printf '%s\tPASS\tpass=%s fail=%s (%s)\n' "$item" "$p" "$f" "$*"
  else printf '%s\tFAIL\tpass=%s fail=%s (%s)\n' "$item" "${p:-?}" "${f:-?}" "$*"; rc=1; fi
}
run A6-15 test/u3a/boundary.test.ts test/u3a/boundary-runtime.test.ts test/u3a/runtime.test.ts
run A6-15a test/u3a/egress-static.test.ts
run A6-15b/c test/u3a/egress-runtime.test.ts
run A6-N1 test/u3a/n1-static.test.ts
run A6-17 test/u3a/windows.test.ts
run A6-18 test/u3a/gate.test.ts test/contract/tools.test.ts
exit "$rc"
