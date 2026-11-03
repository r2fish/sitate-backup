#!/usr/bin/env python3
"""A7(秘密の非出現)の検査を 1 つにまとめた入口(監督の受け入れ用。D-0154)。make の試験から呼ばない。

  check_a7.py fields --records <record.jsonl|dir>... --send-fields app/src/data/send-fields.json --names <期待値の JSON>
      (i) 送った欄の JSON Pointer の集合が送る欄の表の部分集合、(ii) 個人名入りの名前の一致、使用したモデル名、
      表示しなかった応答の件数。--names は test/env/.run/accept-<NAME>/expect/source.json(accept-env.sh up が書く)。
  <秘密の値を標準入力へ> | check_a7.py secrets --records <path>...
      秘密の値(1 行 1 値、8 文字以上)をそのままと base64 で検索。値は標準入力だけで受け、件数だけを出す。
      例(利用者のターミナル): read -rs K; printf '%s\\n' "$K" | check_a7.py secrets --records test/acceptance/records; unset K
  <kind<TAB>値の行を標準入力へ> | check_a7.py redact --in <dir> --out <空の dir>
      鍵 ID・バケット名・アカウント ID・ホスト名・IP(kind は key-id/bucket/account/host/ip)を `<bucket-1>` の形に、長い値から置き換える。

終了コード: 0 合格 / 1 不合格 / 2 入力の誤り。出力に秘密の値・名前の値を出さない。
"""
import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import a7_fields  # noqa: E402
import a7_secrets  # noqa: E402


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)
    f = sub.add_parser("fields")
    f.add_argument("--records", nargs="+", required=True)
    f.add_argument("--send-fields", required=True)
    f.add_argument("--names")
    s = sub.add_parser("secrets")
    s.add_argument("--records", nargs="+", required=True)
    r = sub.add_parser("redact")
    r.add_argument("--in", dest="src", required=True)
    r.add_argument("--out", required=True)
    a = ap.parse_args()
    try:
        if a.cmd == "fields":
            res = a7_fields.check_fields(a.records, a.send_fields, a.names)
            lines, ok = a7_fields.report_fields(res, bool(a.names))
            print("\n".join(lines))
            print("A7 fields " + ("PASS" if ok else "FAIL"))
            return 0 if ok else 1
        if a.cmd == "secrets":
            return a7_secrets.cmd_secrets(a.records)
        return a7_secrets.cmd_redact(a.src, a.out)
    except (OSError, ValueError, KeyError) as e:
        print(f"error: 入力を読めない: {type(e).__name__}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
