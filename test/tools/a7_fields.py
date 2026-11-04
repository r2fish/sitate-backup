#!/usr/bin/env python3
"""A7 の (i)(ii)・モデル名・表示しなかった応答の件数(check_a7.py の fields 部。秘密の値は扱わない)。

記録は試験用ドライバの record.jsonl(1 件 1 行)。LLM API へ送った内容は kind=llm_request の body.messages にあり、
道具の結果の区画 `<sitate-data command="…">JSON</sitate-data>` を抜き出して、送った欄の JSON Pointer の集合を作る。
"""
import json
import os
import re

BLOCK = re.compile(r'<sitate-data command="([^"]*)"[^>]*>\n(.*?)\n</sitate-data>', re.S)
PATH_REF = re.compile(r"^pr:[0-9a-f]{16}$")


def iter_records(paths):
    """(ファイル, 行番号, レコード) を返す。ディレクトリは下の *.jsonl をすべて読む。"""
    files = []
    for p in paths:
        if os.path.isdir(p):
            for root, _, names in os.walk(p):
                files += [os.path.join(root, n) for n in sorted(names) if n.endswith(".jsonl")]
        else:
            files.append(p)
    for f in sorted(files):
        with open(f, encoding="utf-8") as fh:
            for i, line in enumerate(fh, 1):
                line = line.strip()
                if not line:
                    continue
                try:
                    yield f, i, json.loads(line)
                except ValueError:
                    yield f, i, {"kind": "_unparsable"}


def strings_of(v):
    if isinstance(v, str):
        yield v
    elif isinstance(v, list):
        for x in v:
            yield from strings_of(x)
    elif isinstance(v, dict):
        for x in v.values():
            yield from strings_of(x)


def leaf_pointers(v, prefix=""):
    """葉の JSON Pointer を返す(配列の添字は * にする。空の配列・オブジェクトはその場所を葉とする)。値も添える。"""
    if isinstance(v, dict) and v:
        for k, x in v.items():
            yield from leaf_pointers(x, prefix + "/" + k.replace("~", "~0").replace("/", "~1"))
    elif isinstance(v, list) and v:
        for x in v:
            yield from leaf_pointers(x, prefix + "/*")
    else:
        yield prefix, v


def load_table(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)["rows"]


def allowed_for(rows, command, env):
    """この封筒に適用される表の行から (許す Pointer の集合, path_ref の Pointer の集合) を返す。"""
    code = ((env.get("error") or {}).get("code"))
    wcodes = {w.get("code") for w in (env.get("warnings") or []) if isinstance(w, dict)}
    allowed, refs = set(), set()
    for r in rows:
        when = r.get("when")
        if r["where"] == "envelope":
            if when:
                hit = ("error" in when.get("on", []) and code in when["codes"]) or ("warnings" in when.get("on", []) and wcodes & set(when["codes"]))
                if not hit:
                    continue
        elif r["where"] != command:
            continue
        for fld in r["fields"]:
            allowed.add(fld["pointer"])
            if fld.get("transform") == "path_ref":
                refs.add(fld["pointer"])
    return allowed, refs


def is_allowed(pointer, allowed, value=None):
    """表の欄そのもの、または表の欄の下。値が空(null・空の配列・空のオブジェクト)の葉は、表の欄の親なら許す(欄の中身は何も送っていない)。"""
    if any(pointer == a or pointer.startswith(a + "/") for a in allowed):
        return True
    return value in (None, [], {}) and any(a.startswith(pointer + "/") for a in allowed)


def extract_blocks(rec):
    """llm_request の body から区画を (サブコマンド, JSON 文字列) で返す。区画の外の文字列は outside に数える。"""
    body = rec.get("body")
    blocks, outside = [], 0
    msgs = body.get("messages", []) if isinstance(body, dict) else []
    for s in strings_of(msgs):
        found = BLOCK.findall(s)
        blocks += found
        if found and BLOCK.sub("", s).strip():
            outside += 1
    return blocks, outside


def check_fields(paths, table_path, names_path):
    rows = load_table(table_path)
    out = {"requests": 0, "models": set(), "no_model": 0, "unique_blocks": 0, "unparsable_blocks": 0,
           "outside_text_strings": 0, "violations": [], "path_ref_bad": [], "name_hits": [],
           "displayed": 0, "withheld": 0, "files": set(), "truncated_bodies": 0}
    seen = set()
    names = []
    if names_path:
        with open(names_path, encoding="utf-8") as f:
            ex = json.load(f)
        names = [n for n in ex["personal_names"]["strings"] if n]
    name_found = {}
    for f, i, rec in iter_records(paths):
        kind = rec.get("kind")
        if kind == "displayed_reply":
            out["displayed"] += 1
        elif kind == "withheld_reply":
            out["withheld"] += 1
        elif kind == "llm_request":
            out["files"].add(f)
            out["requests"] += 1
            if rec.get("model"):
                out["models"].add(rec["model"])
            else:
                out["no_model"] += 1
            if "body" not in rec:
                out["truncated_bodies"] += 1
                continue
            blob = json.dumps(rec["body"], ensure_ascii=False)
            for n in names:
                if n in blob or json.dumps(n)[1:-1] in blob:
                    name_found.setdefault(n, set()).add(f"{os.path.basename(f)}:{i}")
            blocks, outside = extract_blocks(rec)
            out["outside_text_strings"] += outside
            for cmd, js in blocks:
                if (cmd, js) in seen:
                    continue
                seen.add((cmd, js))
                out["unique_blocks"] += 1
                try:
                    env = json.loads(js)
                except ValueError:
                    out["unparsable_blocks"] += 1
                    continue
                allowed, refs = allowed_for(rows, cmd, env)
                for p, val in leaf_pointers(env):
                    if not is_allowed(p, allowed, val):
                        out["violations"].append((cmd, p))
                    # null は値の無い欄(ラージオブジェクトの要素の path など。仕様どおり)で、path_ref の形の外れに数えない
                    if p in refs and val is not None and not (isinstance(val, str) and PATH_REF.match(val)):
                        out["path_ref_bad"].append((cmd, p))
    out["name_hits"] = sorted((n, len(v)) for n, v in name_found.items())
    out["names_checked"] = len(names)
    return out


def report_fields(r, names_given):
    """判定の行(件数と Pointer だけ。値は出さない)を返し、(行, 合否) を返す。"""
    lines, ok = [], True
    viol = sorted(set(r["violations"]))
    lines.append(f"A7-2 llm_request={r['requests']} 区画(重複除く)={r['unique_blocks']} 解析不能の区画={r['unparsable_blocks']} 本文を省いた記録={r['truncated_bodies']}")
    lines.append(f"A7-2 表に無い欄(サブコマンド, Pointer)の種類={len(viol)} " + ("PASS" if not viol and r["unique_blocks"] > 0 else "FAIL"))
    for cmd, p in viol:
        lines.append(f"  violation {cmd} {p}")
    lines.append(f"A7-2 path_ref の欄が pr:<16 桁> でない={len(set(r['path_ref_bad']))} " + ("PASS" if not r["path_ref_bad"] else "FAIL"))
    lines.append(f"A7-2 参考: 区画の外に文字を持つ文字列={r['outside_text_strings']}(区画の外に文は置かない規則。件数だけ)")
    ok &= not viol and r["unique_blocks"] > 0 and not r["path_ref_bad"] and r["unparsable_blocks"] == 0 and r["truncated_bodies"] == 0
    if names_given:
        lines.append(f"A7-3 期待値の名前={r['names_checked']} 一致した名前={len(r['name_hits'])} " + ("PASS" if not r["name_hits"] and r["names_checked"] > 0 else "FAIL"))
        for i, (_, c) in enumerate(r["name_hits"], 1):
            lines.append(f"  name#{i} 記録の行={c}")
        ok &= not r["name_hits"] and r["names_checked"] > 0
    else:
        lines.append("A7-3 --names が無いので未実施")
        ok = False
    models = sorted(r["models"])
    lines.append(f"A7-4 使用したモデル名={models} model 欄が空の送信={r['no_model']} " + ("PASS" if models and not r["no_model"] else "FAIL"))
    ok &= bool(models) and not r["no_model"]
    lines.append(f"A7 表示した応答={r['displayed']} 表示しなかった応答={r['withheld']}(件数だけ。判定に数えない)")
    return lines, ok
