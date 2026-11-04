#!/usr/bin/env python3
"""A7 の秘密の検索と伏せ字(check_a7.py の secrets・redact 部)。

値は標準入力でだけ受ける(引数・ファイル・環境変数に残さない)。出力は件数と連番だけで、値も値の一部も出さない。
"""
import base64
import json
import os
import sys

MIN_LEN = 8
SKIP_PREFIX = "-----"  # PEM の見出し行は秘密ではない


def read_needles(stream):
    """標準入力の行を検索語にする。短すぎる行と PEM の見出しは数えて飛ばす。"""
    needles, skipped = [], 0
    for line in stream.read().split("\n"):
        line = line.rstrip("\r")
        if not line:
            continue
        if line.startswith(SKIP_PREFIX) or len(line) < MIN_LEN:
            skipped += 1
            continue
        needles.append(line)
    return needles, skipped


def b64_forms(raw):
    """base64 の表記。埋め込まれた位置(3 通りの桁の揃え)でも見つかるよう、前後の不安定な文字を落とした形を返す。"""
    data = raw.encode("utf-8")
    forms = set()
    for k in (0, 1, 2):
        enc = base64.b64encode(b"\0" * k + data).decode("ascii").rstrip("=")
        head = {0: 0, 1: 2, 2: 3}[k]
        tail = {0: 0, 1: 2, 2: 3}[(k + len(data)) % 3]
        core = enc[head:len(enc) - tail] if tail else enc[head:]
        if len(core) >= MIN_LEN:
            forms.add(core.encode("ascii"))
    return forms


def forms_of(raw):
    plain = {raw.encode("utf-8"), json.dumps(raw, ensure_ascii=False)[1:-1].encode("utf-8")}
    return plain, b64_forms(raw)


def walk_files(paths):
    for p in paths:
        if os.path.isdir(p):
            for root, _, names in os.walk(p):
                for n in sorted(names):
                    yield os.path.join(root, n)
        else:
            yield p


def count_in(data, form):
    n, start = 0, 0
    while True:
        i = data.find(form, start)
        if i < 0:
            return n
        n += 1
        start = i + 1


def search(paths, needles):
    """(ファイル数, 検索語ごとの (そのまま, base64) の件数の並び)を返す。"""
    forms = [forms_of(n) for n in needles]
    counts = [[0, 0] for _ in needles]
    nfiles = 0
    for f in walk_files(paths):
        try:
            with open(f, "rb") as fh:
                data = fh.read()
        except OSError:
            continue
        nfiles += 1
        for i, (plain, b64) in enumerate(forms):
            counts[i][0] += sum(count_in(data, x) for x in plain)
            counts[i][1] += sum(count_in(data, x) for x in b64)
    return nfiles, counts


def cmd_secrets(paths):
    needles, skipped = read_needles(sys.stdin)
    if not needles:
        print("error: 標準入力に検索語(8 文字以上の行)が無い", file=sys.stderr)
        return 2
    nfiles, counts = search(paths, needles)
    total = 0
    print(f"files={nfiles} needles={len(needles)} skipped_short_or_header={skipped}")
    for i, (a, b) in enumerate(counts, 1):
        total += a + b
        print(f"secret#{i} plain={a} base64={b}")
    print(f"A7-1 一致の合計={total} " + ("PASS" if total == 0 and nfiles > 0 else "FAIL"))
    return 0 if total == 0 and nfiles > 0 else 1


KINDS = ("key-id", "bucket", "account", "host", "ip")


def read_pairs(stream):
    pairs = []
    for line in stream.read().split("\n"):
        line = line.rstrip("\r")
        if not line:
            continue
        kind, _, value = line.partition("\t")
        if kind not in KINDS or len(value) < 3:
            raise ValueError("kind<TAB>値 の行(kind は " + "/".join(KINDS) + "、値は 3 文字以上)")
        pairs.append((kind, value))
    return pairs


def cmd_redact(src, dst):
    try:
        pairs = read_pairs(sys.stdin)
    except ValueError as e:
        print(f"error: {e}", file=sys.stderr)
        return 2
    if os.path.exists(dst) and os.listdir(dst):
        print("error: --out が空でない", file=sys.stderr)
        return 2
    # 連番は種類ごとに入力順。置き換えは長い値から(GB-136)。
    seq, label = {}, {}
    for kind, value in pairs:
        if value not in label:
            seq[kind] = seq.get(kind, 0) + 1
            label[value] = f"<{kind}-{seq[kind]}>"
    order = sorted(label, key=len, reverse=True)
    hits = {l: 0 for l in label.values()}
    nfiles = 0
    for f in walk_files([src]):
        rel = os.path.relpath(f, src)
        with open(f, "rb") as fh:
            data = fh.read()
        for v in order:
            b = v.encode("utf-8")
            c = count_in(data, b)
            if c:
                hits[label[v]] += c
                data = data.replace(b, label[v].encode("utf-8"))
        out = os.path.join(dst, rel)
        os.makedirs(os.path.dirname(out), exist_ok=True)
        with open(out, "wb") as fh:
            fh.write(data)
        nfiles += 1
    left = 0
    for v in order:
        nf, c = search([dst], [v]) if len(v) >= MIN_LEN else (0, [[0, 0]])
        left += c[0][0]
    print(f"files={nfiles} values={len(order)}")
    for l in sorted(hits):
        print(f"{l} replaced={hits[l]}")
    print(f"残り(8 文字以上の値の再検索)={left} " + ("PASS" if left == 0 else "FAIL"))
    return 0 if left == 0 else 1
