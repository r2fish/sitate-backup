#!/usr/bin/env python3
"""送る欄の表の /result 配下の Pointer が結果スキーマにあることの照合(監督の受け入れ用。D-0154)。

make の試験から呼ばない。出力は件数、差分のキー、合否だけ。

使い方: check_send_fields_schema.py --send-fields app/src/data/send-fields.json --schema-dir internal/contract/schema
終了コード: 0 全件あり / 1 欠けあり / 2 入力の誤り
"""
import argparse
import json
import os
import re
import sys


def load_defs(schema_dir):
    defs = {}
    for name in ("cli/results.json", "cli/common.json", "data/change.json"):
        with open(os.path.join(schema_dir, name), encoding="utf-8") as f:
            defs[name] = json.load(f)["$defs"]
    return defs


def deref(schema, defs, current):
    """$ref を辿り、(スキーマ, その定義の属するファイル) を返す。"""
    while isinstance(schema, dict) and "$ref" in schema:
        ref = schema["$ref"]
        key = ref.rsplit("/", 1)[-1]
        if not ref.startswith("#"):
            current = next(n for n in defs if ref.split("#")[0].endswith(n))
        schema = defs[current][key]
    return schema, current


def alternatives(schema, defs, current):
    schema, current = deref(schema, defs, current)
    found = [(schema, current)]
    if isinstance(schema, dict):
        for key in ("anyOf", "oneOf", "allOf"):
            for sub in schema.get(key, []):
                found += alternatives(sub, defs, current)
    return found


def has_pointer(schema, tokens, defs, current):
    if not tokens:
        return True
    head, rest = tokens[0], tokens[1:]
    for alt, cur in alternatives(schema, defs, current):
        if not isinstance(alt, dict):
            continue
        if head == "*" and "items" in alt and has_pointer(alt["items"], rest, defs, cur):
            return True
        if head in alt.get("properties", {}) and has_pointer(alt["properties"][head], rest, defs, cur):
            return True
        extra = alt.get("additionalProperties")
        if isinstance(extra, dict) and has_pointer(extra, rest, defs, cur):
            return True
        if alt.get("type") == "object" and not alt.get("properties") and extra is not False:
            return True
    return False


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--send-fields", required=True)
    parser.add_argument("--schema-dir", required=True)
    args = parser.parse_args()
    with open(args.send_fields, encoding="utf-8") as f:
        rows = json.load(f)["rows"]
    defs = load_defs(args.schema_dir)
    results = "cli/results.json"

    checked = 0
    missing = []
    for row in rows:
        name = re.sub(r"[ -]", "_", row["where"])
        for field in row["fields"]:
            pointer = field["pointer"]
            if not pointer.startswith("/result"):
                continue
            checked += 1
            if name not in defs[results] or not has_pointer(defs[results][name], pointer.split("/")[2:], defs, results):
                missing.append((row["where"], pointer))
    print(f"checked={checked}")
    print(f"missing={len(missing)}")
    for where, pointer in missing:
        print(f"missing {where} {pointer}")
    print("PASS" if not missing else "FAIL")
    return 0 if not missing else 1


if __name__ == "__main__":
    sys.exit(main())
