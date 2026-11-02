#!/usr/bin/env python3
"""契約試験が判定まで届いたかの集計(監督の受け入れ用)。

Go の -cover は試験のファイルを計らないので、go test -json の結果から、失敗した行より手前にある
t.Error・t.Fatal の行を数える。make の試験から呼ばない。

使い方:
  go test -json -count=1 -run '^TestContract' ./internal/<部品>/ > 結果.json
  cov_stats.py --root . --json 結果.json [--package <部品>]

判定: 失敗した試験か、ビルドに失敗して走らなかったパッケージがあれば未完了(未実装の印で止まった試験は判定に届いていない)。
読み方の限界: 本体の行だけを数え、補助関数の中の判定は数えない。合格した試験の途中の return は見分けない。
手前の行が実行されたとは限らない(準備の失敗を見る行かもしれない)ので、届いた数は上限の目安である。

終了コード: 0 失敗なし / 1 判定に届かない試験あり / 2 入力の誤り
"""
import argparse
import collections
import glob
import json
import os
import re
import sys

MARKER = "NOT_IMPLEMENTED"
ASSERT = re.compile(r"\bt\.(Error|Errorf|Fatal|Fatalf)\(")
TEST_FUNC = re.compile(r"func (TestContract\w+)\(")


def read_spans(root, package):
    """試験の名前から (ファイル名, 開始行, 終了行, 判定の行, 部品) を引く表を返す。"""
    spans = {}
    pattern = os.path.join(root, "internal", package or "*", "contract*_test.go")
    for path in glob.glob(pattern):
        pkg = os.path.basename(os.path.dirname(path))
        if pkg == "contract":
            continue
        with open(path, encoding="utf-8") as f:
            lines = f.read().split("\n")
        i = 0
        while i < len(lines):
            m = TEST_FUNC.match(lines[i])
            if m:
                j = i
                while j < len(lines) and lines[j] != "}":
                    j += 1
                asserts = [n + 1 for n in range(i, j) if ASSERT.search(lines[n])]
                spans[m.group(1)] = (os.path.basename(path), i + 1, j + 1, asserts, pkg)
                i = j
            i += 1
    return spans


def read_results(path):
    """go test -json の結果から、最上位の試験ごとの (状態, 出力) と、試験を 1 件も走らせずに失敗したパッケージを返す。"""
    outputs = collections.defaultdict(list)
    status = {}
    failed_packages = set()
    packages_with_tests = set()
    with open(path, encoding="utf-8") as f:
        for line in f:
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                continue
            test = event.get("Test")
            action = event.get("Action")
            package = event.get("Package", "")
            if not test:
                if action == "fail" and package:
                    failed_packages.add(package)
                continue
            packages_with_tests.add(package)
            top = test.split("/")[0]
            if action == "output":
                outputs[top].append(event.get("Output", ""))
            elif action in ("pass", "fail", "skip") and "/" not in test:
                status[top] = action
    return status, outputs, failed_packages - packages_with_tests


def reached_asserts(span, text):
    """失敗した行より手前の判定の数を返す。失敗の行が本体に無ければ None。"""
    file_name, start, end, asserts, _ = span
    found = [int(x) for x in re.findall(re.escape(file_name) + r":(\d+):", text)]
    inside = [x for x in found if start <= x <= end]
    if not inside:
        return None
    first = min(inside)
    return sum(1 for x in asserts if x < first)


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--root", required=True)
    parser.add_argument("--json", required=True)
    parser.add_argument("--package")
    args = parser.parse_args()

    spans = read_spans(args.root, args.package)
    status, outputs, not_run = read_results(args.json)
    if not status and not not_run:
        print("error: go test -json の試験の結果が無い", file=sys.stderr)
        return 2

    counts = collections.Counter()
    per_package = collections.defaultdict(lambda: [0, 0, 0])
    unreached = []
    total = reached = 0
    for test, state in sorted(status.items()):
        if test not in spans:
            continue
        counts[state] += 1
        if state != "fail":
            continue
        span = spans[test]
        text = "".join(outputs[test])
        n = len(span[3])
        r = reached_asserts(span, text)
        total += n
        reached += r or 0
        row = per_package[span[4]]
        row[0] += 1
        row[1] += n
        row[2] += r or 0
        kind = "marker" if MARKER in text else "other"
        unreached.append((span[4], test, kind, "?" if r is None else r, n))

    print("tests " + " ".join(f"{k}={counts[k]}" for k in ("pass", "fail", "skip")))
    print(f"failed_asserts_reached={reached}/{total}")
    for pkg, row in sorted(per_package.items()):
        print(f"package {pkg} failed={row[0]} asserts={row[1]} reached={row[2]}")
    for pkg, test, kind, r, n in unreached:
        print(f"unreached {pkg} {test} {kind} {r}/{n}")
    # ビルドに失敗したパッケージは試験が 1 件も走らない。合格に数えない
    for package in sorted(not_run):
        print(f"not_run {package}")
    ok = not unreached and not not_run
    print("PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
