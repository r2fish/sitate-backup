#!/usr/bin/env python3
"""送る欄の表のデータと仕様書 3.7 の表の照合(監督の受け入れ用。D-0154)。

make の試験から呼ばない。仕様書のパスは必須の引数で受け、既定のパスを持たない。
出力は件数、差分のキー、合否、仕様書の版だけで、仕様書の文を出さない。

使い方: check_send_fields.py --spec <仕様書> --send-fields app/src/data/send-fields.json
終了コード: 0 一致 / 1 差分あり / 2 入力の誤り
"""
import argparse
import json
import re
import sys

TABLE_HEADER = "| サブコマンド | 送る欄(JSON Pointer) | 除く欄 |"


def split_row(row):
    return [c.strip() for c in row.split("|")[1:-1]]


def expand(cell):
    """セルを展開し (Pointer の集合, path_ref に置き換える Pointer の集合) を返す。

    `/a/b`・`c` は /a/c、`/a/b`・`c/d`・`e` は /a/c/e(`/` の無い欄は直前に解いた欄の兄弟、`/` のある欄は直前の絶対の欄の親から)、<i> は * にする。
    """
    pointers = set()
    refs = set()
    # 丸括弧の中は注記で欄を増やさない。path_ref の注記だけ直前の欄の印として読む
    marked = re.sub(r"\(([^)]*)\)", lambda m: "\0" if "path_ref" in m.group(1) else "", cell)
    for part in marked.split("、"):
        parent = None  # 直前の絶対の欄の親(`/` を含む相対の欄はここから)
        last = None  # 直前に解いた欄の親(`/` を含まない欄はその兄弟)
        for token, mark in re.findall(r"`([^`]+)`(\0?)", part):
            if token.startswith("/"):
                pointer = token
                parent = last = token.rsplit("/", 1)[0]
            elif parent is not None and not token.startswith("-"):
                pointer = (parent if "/" in token else last) + "/" + token
                last = pointer.rsplit("/", 1)[0]
            else:
                continue
            pointer = re.sub(r"<[a-z]+>", "*", pointer)
            pointers.add(pointer)
            if mark:
                refs.add(pointer)
    return pointers, refs


def row_key(cell):
    """表の 1 列目から (where, 原因コードの集合, 対象) を導く。

    where: `すべて(…)` は envelope、それ以外は最初のコード片(`status`(1) は status)。
    原因コード: コード片のうち大文字と `_` だけの語。対象: `error.code` なら error、`warnings[].code` なら warnings。
    """
    tokens = re.findall(r"`([^`]+)`", cell)
    where = "envelope" if cell.startswith("すべて") else (tokens[0] if tokens else cell)
    codes = {t for t in tokens if re.fullmatch(r"[A-Z][A-Z0-9_]+", t)}
    on = set()
    if "error.code" in tokens:
        on.add("error")
    if "warnings[].code" in tokens:
        on.add("warnings")
    return where, codes, on


def read_spec_table(lines):
    if TABLE_HEADER not in lines:
        print("error: 仕様書に送る欄の表の見出しが無い", file=sys.stderr)
        raise SystemExit(2)
    start = lines.index(TABLE_HEADER) + 2
    rows = []
    for line in lines[start:]:
        if not line.startswith("|"):
            break
        cells = split_row(line)
        send, refs = expand(cells[1])
        excluded = expand(cells[2])[0] if len(cells) > 2 else set()
        rows.append((send, refs, excluded, row_key(cells[0])))
    return rows


def spec_version(lines):
    for line in lines:
        m = re.match(r"- 版: ([0-9.]+)", line)
        if m:
            return m.group(1)
    return "unknown"


def compare(rows, data_rows):
    """行ごとの差分を (行のキー, 種類, Pointer など) の並びで返す。1 列目から導いた where・原因コード・対象も照らす。"""
    diffs = []
    for (send, refs, excluded, (spec_where, spec_codes, spec_on)), row in zip(rows, data_rows):
        pointers = {f["pointer"] for f in row["fields"]}
        transforms = {f["pointer"] for f in row["fields"] if f.get("transform") == "path_ref"}
        where = row["where"]
        when = row.get("when") or {}
        if where != spec_where:
            diffs.append((spec_where, "where_mismatch", where))
        if set(when.get("codes", [])) != spec_codes:
            diffs.append((spec_where, "codes_mismatch", ",".join(sorted(set(when.get("codes", [])) ^ spec_codes))))
        if spec_codes and set(when.get("on", [])) != spec_on:
            diffs.append((spec_where, "on_mismatch", ",".join(sorted(set(when.get("on", [])) ^ spec_on))))
        diffs += [(where, "spec_only", p) for p in sorted(send - pointers)]
        diffs += [(where, "data_only", p) for p in sorted(pointers - send)]
        diffs += [(where, "excluded_sent", p) for p in sorted(pointers & excluded)]
        diffs += [(where, "path_ref_missing", p) for p in sorted(refs - transforms)]
        diffs += [(where, "path_ref_extra", p) for p in sorted(transforms - refs)]
    return diffs


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--spec", required=True)
    parser.add_argument("--send-fields", required=True)
    args = parser.parse_args()
    try:
        with open(args.spec, encoding="utf-8") as f:
            lines = f.read().split("\n")
        with open(args.send_fields, encoding="utf-8") as f:
            data_rows = json.load(f)["rows"]
    except (OSError, ValueError, KeyError) as e:
        print(f"error: 入力を読めない: {type(e).__name__}", file=sys.stderr)
        return 2

    rows = read_spec_table(lines)
    diffs = compare(rows, data_rows)
    ok = len(rows) == len(data_rows) and not diffs
    print(f"spec_version={spec_version(lines)}")
    print(f"rows spec={len(rows)} data={len(data_rows)}")
    print(f"pointers spec={sum(len(r[0]) for r in rows)} data={sum(len(r['fields']) for r in data_rows)}")
    print(f"path_ref spec={sum(len(r[1]) for r in rows)} data={sum(1 for r in data_rows for f in r['fields'] if f.get('transform'))}")
    print(f"diffs={len(diffs)}")
    for where, kind, pointer in diffs:
        print(f"{kind} {where} {pointer}")
    print("PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
