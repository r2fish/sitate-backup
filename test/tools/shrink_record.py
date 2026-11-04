#!/usr/bin/env python3
"""受入試験の記録(record.jsonl)を、公開リポジトリに置く形に縮めて伏せ字にする道具(監督の受け入れ用)。make の試験から呼ばない。

  shrink_record.py shrink --in-root <原本の根> --out-root <出力の根> --map <伏せ字の一覧の JSON の出力先> [--machine <名前>]...
      <原本の根> の下の全 record.jsonl を集め(まず全部から伏せる値を集める。記録をまたいで同じ値に同じ連番)、
      同じ相対パスで縮めた記録を <出力の根> に書く。--map には集めた値そのものを書く(リポジトリに置かない)。
  shrink_record.py expand --in <縮めた記録> --out <戻した記録>
      各 llm_request の body の全文(messages・tools・system)を組み直す。

縮め: llm_request の body.messages は要求ごとに前の要求の messages を先頭に含む累積の形なので、前の要求の messages を
先頭に含むときは差分だけを残し、引き継いだ件数を行の欄 messages_prefix に置く。含まないときは全文を残す(messages_prefix: 0)。
body の tools・system は前の要求と同じなら省き、行の欄 tools_same_as_prev・system_same_as_prev を true にする。
ほかの kind の行と llm_request のほかの欄は変えない。

伏せ字: 鍵 ID・バケット名・アカウント ID・ホスト名・IP を <key-id-1>・<bucket-1>・<account-1>・<host-1>・<ip-1> の形に、
長い値から置き換える。作業端末のホームの絶対パス(/Users/<ユーザー名>)は <home>、--machine で渡した機械の名前は <machine>。
試験環境のコンテナの役名(source・backup・dest)、予約済みの例示のドメイン(.invalid)、repo_id・版 ID などは伏せない。
出力に値を出さない(件数だけ)。
"""
import argparse
import json
import os
import re
import sys

KINDS = ("key-id", "bucket", "account", "host", "ip")
ROLE_NAMES = {"source", "backup", "dest", "hook", "localhost"}
ALNUM = re.compile(r"^[A-Za-z0-9]+$")
KEY_ID_FORM = re.compile(r"(?<![0-9A-Za-z])004[0-9a-f]{22}(?![0-9A-Za-z])")
# JSON の欄(文字列の中に埋まって \" になっていてもよい): "名前":"値"
FIELD = r'\\*"{names}\\*"\s*:\s*\\*"([^"\\]*)'
KEY_FIELD = re.compile(FIELD.format(names=r"(?:key_id|keyId|access_key_id|accessKeyId)"))
ACCOUNT_FIELD = re.compile(FIELD.format(names=r"(?:account_id|accountId)"))
BUCKET_FIELD = re.compile(FIELD.format(names=r"bucket(?:_name|Name)?"))
BUCKET_PROSE = re.compile(r"バケット名は\s*([A-Za-z0-9][A-Za-z0-9._-]{2,62})")
S3_URL = re.compile(r"s3://([A-Za-z0-9][A-Za-z0-9._-]{2,62})")
URL_HOST = re.compile(r"[a-z][a-z0-9+.-]*://(?:[^/\s@\"\\]*@)?([A-Za-z0-9][A-Za-z0-9.-]*)")
B2_HOST = re.compile(r"(?<![\w.-])((?:[A-Za-z0-9-]+\.)+backblazeb2\.com)(?![\w-])")
IPV4 = re.compile(r"(?<![\d.])(\d{1,3}(?:\.\d{1,3}){3})(?![\d.])")
HOME = re.compile(r"/Users/([A-Za-z0-9._-]+)")


def _ok_id(v, minlen=8):
    return bool(v) and len(v) >= minlen and bool(ALNUM.match(v))


def _is_ip(v):
    p = v.split(".")
    return len(p) == 4 and all(x.isdigit() and int(x) < 256 for x in p)


def collect_values(text):
    """1 つの記録の本文から (種類, 値) を出現順に返す(重複あり)。"""
    found = []
    for m in KEY_ID_FORM.finditer(text):
        found.append(("key-id", m.group(0)))
        found.append(("account", m.group(0)[3:15]))
    for m in KEY_FIELD.finditer(text):
        if _ok_id(m.group(1), 12):
            found.append(("key-id", m.group(1)))
    for m in ACCOUNT_FIELD.finditer(text):
        if _ok_id(m.group(1)):
            found.append(("account", m.group(1)))
    for rx in (BUCKET_FIELD, BUCKET_PROSE, S3_URL):
        for m in rx.finditer(text):
            v = m.group(1)
            if v and "<" not in v and len(v) >= 3 and re.match(r"^[A-Za-z0-9][A-Za-z0-9._-]*$", v):
                found.append(("bucket", v))
    for m in URL_HOST.finditer(text):
        h = m.group(1).rstrip(".")
        if _is_ip(h):
            found.append(("ip", h))
        elif "." in h and h not in ROLE_NAMES and not h.endswith(".invalid"):
            found.append(("host", h))
    for m in B2_HOST.finditer(text):
        found.append(("host", m.group(1)))
    for m in IPV4.finditer(text):
        if _is_ip(m.group(1)):
            found.append(("ip", m.group(1)))
    return found


def build_map(texts, machines=()):
    """全記録をまたいで、種類ごとに出現順の連番を当てた {種類: {値: 置換後}} を返す。"""
    maps = {k: {} for k in KINDS}
    for t in texts:
        for kind, v in collect_values(t):
            if v not in maps[kind]:
                maps[kind][v] = f"<{kind}-{len(maps[kind]) + 1}>"
    # 鍵 ID の一部として現れるだけのアカウント ID は、鍵 ID を先に置き換えるので残りの出現だけが対象になる
    return {"values": maps, "machines": sorted(set(machines))}


def redact_text(text, rmap):
    """値を長い値から順に置き換える(GB-136)。ホームの絶対パスは <home>、機械の名前は <machine>。"""
    pairs = [(v, lab) for m in rmap["values"].values() for v, lab in m.items()]
    pairs.sort(key=lambda p: (-len(p[0]), p[0]))
    for v, lab in pairs:
        text = text.replace(v, lab)
    text = HOME.sub(lambda m: "/Users/<user>" if m.group(1).startswith("<") else "<home>", text)
    for name in sorted(rmap.get("machines", []), key=len, reverse=True):
        text = text.replace(name, "<machine>")
    return text


def _dump(obj):
    return json.dumps(obj, ensure_ascii=False, separators=(",", ":"))


def shrink_lines(lines, rmap):
    """行(改行なし)の列を、伏せ字を当てて縮めた行の列にする。"""
    out, prev = [], None
    for line in lines:
        text = redact_text(line, rmap)
        if not text.strip():
            out.append(text)
            continue
        try:
            row = json.loads(text)
        except ValueError:
            out.append(text)
            continue
        body = row.get("body") if isinstance(row, dict) and row.get("kind") == "llm_request" else None
        if not isinstance(body, dict):
            out.append(text)
            continue
        full = body
        nb = dict(body)
        if isinstance(body.get("messages"), list):
            pm = prev.get("messages") if prev else None
            n = len(pm) if isinstance(pm, list) and body["messages"][:len(pm)] == pm else 0
            nb["messages"] = body["messages"][n:]
            row["messages_prefix"] = n
        for key in ("tools", "system"):
            if prev is not None and key in body and key in prev and body[key] == prev[key]:
                del nb[key]
                row[key + "_same_as_prev"] = True
        row["body"] = nb
        out.append(_dump(row))
        prev = full
    return out


def expand_lines(lines):
    """縮めた行の列から、各 llm_request の body の全文を組み直した行の列を返す。"""
    out, prev = [], None
    for line in lines:
        if not line.strip():
            out.append(line)
            continue
        try:
            row = json.loads(line)
        except ValueError:
            out.append(line)
            continue
        body = row.get("body") if isinstance(row, dict) and row.get("kind") == "llm_request" else None
        if not isinstance(body, dict):
            out.append(line)
            continue
        body = dict(body)
        n = row.pop("messages_prefix", None)
        if n is not None:
            if n:
                pm = prev.get("messages") if prev else None
                if not isinstance(pm, list) or len(pm) < n:
                    raise ValueError("messages_prefix が前の要求の件数を超える")
                body["messages"] = pm[:n] + body.get("messages", [])
        for key in ("tools", "system"):
            if row.pop(key + "_same_as_prev", False):
                if prev is None or key not in prev:
                    raise ValueError(key + "_same_as_prev の前の要求が無い")
                body[key] = prev[key]
        row["body"] = body
        out.append(_dump(row))
        prev = body
    return out


def read_lines(path):
    with open(path, encoding="utf-8") as f:
        return f.read().split("\n")[:-1]


def write_lines(path, lines):
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        for ln in lines:
            f.write(ln + "\n")


def find_records(root):
    found = []
    for d, _, fs in os.walk(root):
        if "record.jsonl" in fs:
            found.append(os.path.relpath(os.path.join(d, "record.jsonl"), root))
    return sorted(found)


def cmd_shrink(a):
    rels = find_records(a.in_root)
    if not rels:
        print("error: record.jsonl が無い", file=sys.stderr)
        return 2
    texts = {}
    for r in rels:
        with open(os.path.join(a.in_root, r), encoding="utf-8") as f:
            texts[r] = f.read()
    rmap = build_map([texts[r] for r in rels], a.machine)
    os.makedirs(os.path.dirname(os.path.abspath(a.map)), exist_ok=True)
    with open(a.map, "w", encoding="utf-8") as f:
        json.dump(rmap, f, ensure_ascii=False, indent=1)
    os.chmod(a.map, 0o600)
    before = after = 0
    for r in rels:
        lines = texts[r].split("\n")[:-1]
        shrunk = shrink_lines(lines, rmap)
        write_lines(os.path.join(a.out_root, r), shrunk)
        before += len(texts[r].encode("utf-8"))
        after += sum(len(x.encode("utf-8")) + 1 for x in shrunk)
    counts = " ".join(f"{k}={len(v)}" for k, v in rmap["values"].items())
    print(f"files={len(rels)} bytes_before={before} bytes_after={after} redactions: {counts}")
    return 0


def cmd_expand(a):
    write_lines(a.out, expand_lines(read_lines(a.src)))
    return 0


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("shrink")
    s.add_argument("--in-root", required=True)
    s.add_argument("--out-root", required=True)
    s.add_argument("--map", required=True)
    s.add_argument("--machine", action="append", default=[])
    e = sub.add_parser("expand")
    e.add_argument("--in", dest="src", required=True)
    e.add_argument("--out", required=True)
    a = ap.parse_args()
    try:
        return cmd_shrink(a) if a.cmd == "shrink" else cmd_expand(a)
    except (OSError, ValueError) as ex:
        print(f"error: {type(ex).__name__}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
