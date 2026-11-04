"""shrink_record.py の単体試験(python3 -m unittest test/tools/test_shrink_record.py)。値は合成のもの。"""
import hashlib
import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import shrink_record as sr  # noqa: E402

KEY = "004" + "a1b2c3d4e5f6" + "0123456789"  # アカウント ID 部分は a1b2c3d4e5f6
HOST = "s3.eu-test-001.backblazeb2.com"
BUCKET = "bkt-synthetic-0001"
BUCKET2 = "bkt-synthetic-0001-extra"


def req(n, messages, tools=None, system=None, extra=None):
    body = {"model": "m", "max_tokens": 10}
    if system is not None:
        body["system"] = system
    body["messages"] = messages
    if tools is not None:
        body["tools"] = tools
    row = {"at": "t", "kind": "llm_request", "n": n, "model": "m", "key_ok": True, "body": body}
    row.update(extra or {})
    return json.dumps(row, ensure_ascii=False, separators=(",", ":"))


def msg(i):
    return {"role": "user" if i % 2 == 0 else "assistant", "content": f"m{i}"}


def sha(lines):
    canon = [json.dumps(json.loads(x), sort_keys=True, ensure_ascii=False) for x in lines]
    return hashlib.sha256("\n".join(canon).encode()).hexdigest()


EMPTY_MAP = {"values": {k: {} for k in sr.KINDS}, "machines": []}


class ShrinkExpand(unittest.TestCase):
    def lines(self):
        tools = [{"name": "t1"}]
        system = [{"type": "text", "text": "sys"}]
        return [
            json.dumps({"at": "t", "kind": "header", "x": 1}),
            req(1, [msg(0)], tools, system),
            json.dumps({"at": "t", "kind": "run_start", "id": "i0"}),
            req(2, [msg(0), msg(1), msg(2)], tools, system),
            req(3, [msg(0), msg(1), msg(2), msg(3)], tools, [{"type": "text", "text": "other"}]),
        ]

    def test_roundtrip_restores_full_body(self):
        orig = self.lines()
        shrunk = sr.shrink_lines(orig, EMPTY_MAP)
        back = sr.expand_lines(shrunk)
        self.assertEqual([json.loads(x) for x in back], [json.loads(x) for x in orig])
        self.assertEqual(sha(back), sha(orig))

    def test_diff_and_markers(self):
        shrunk = [json.loads(x) for x in sr.shrink_lines(self.lines(), EMPTY_MAP)]
        r1, r2, r3 = shrunk[1], shrunk[3], shrunk[4]
        self.assertEqual(r1["messages_prefix"], 0)
        self.assertEqual(len(r1["body"]["messages"]), 1)
        self.assertEqual(r2["messages_prefix"], 1)
        self.assertEqual(r2["body"]["messages"], [msg(1), msg(2)])
        self.assertTrue(r2["tools_same_as_prev"] and r2["system_same_as_prev"])
        self.assertNotIn("tools", r2["body"])
        self.assertEqual(r3["messages_prefix"], 3)
        self.assertTrue(r3["tools_same_as_prev"])
        self.assertNotIn("system_same_as_prev", r3)  # system が違うので全文
        self.assertIn("system", r3["body"])

    def test_other_rows_unchanged(self):
        orig = self.lines()
        shrunk = sr.shrink_lines(orig, EMPTY_MAP)
        self.assertEqual(shrunk[0], orig[0])
        self.assertEqual(shrunk[2], orig[2])

    def test_other_llm_request_fields_kept(self):
        shrunk = json.loads(sr.shrink_lines(self.lines(), EMPTY_MAP)[3])
        for k in ("at", "kind", "n", "model", "key_ok"):
            self.assertIn(k, shrunk)

    def test_broken_prefix_keeps_full_text(self):
        a = req(1, [msg(0), msg(1)])
        b = req(2, [msg(0), {"role": "assistant", "content": "changed"}, msg(2)])
        shrunk = [json.loads(x) for x in sr.shrink_lines([a, b], EMPTY_MAP)]
        self.assertEqual(shrunk[1]["messages_prefix"], 0)
        self.assertEqual(len(shrunk[1]["body"]["messages"]), 3)
        self.assertEqual(sha(sr.expand_lines(sr.shrink_lines([a, b], EMPTY_MAP))), sha([a, b]))

    def test_shorter_following_request_keeps_full_text(self):
        a = req(1, [msg(0), msg(1), msg(2)])
        b = req(2, [msg(0)])
        shrunk = [json.loads(x) for x in sr.shrink_lines([a, b], EMPTY_MAP)]
        self.assertEqual(shrunk[1]["messages_prefix"], 0)
        self.assertEqual(sha(sr.expand_lines(sr.shrink_lines([a, b], EMPTY_MAP))), sha([a, b]))

    def test_expand_rejects_bad_prefix(self):
        bad = json.dumps({"kind": "llm_request", "messages_prefix": 5, "body": {"messages": []}})
        with self.assertRaises(ValueError):
            sr.expand_lines([req(1, [msg(0)]), bad])


class Redaction(unittest.TestCase):
    TEXT = (f'{{"kind":"question","text":"https://{HOST}、バケット名は {BUCKET} です"}}\n'
            f'{{"env":"{{\\"key_id\\":\\"{KEY}\\",\\"bucket\\":\\"{BUCKET2}\\"}}"}}\n'
            'ssh://admin@203.0.113.10:22 ssh://source ssh://dest adma02bs /Users/someone/work r-20261104T220655Z-1341\n')

    def rmap(self, machines=()):
        return sr.build_map([self.TEXT], machines)

    def test_collected_kinds_and_numbering(self):
        m = self.rmap()["values"]
        self.assertEqual(m["host"], {HOST: "<host-1>"})
        self.assertEqual(m["ip"], {"203.0.113.10": "<ip-1>"})
        self.assertEqual(set(m["bucket"].values()), {"<bucket-1>", "<bucket-2>"})
        self.assertEqual(m["key-id"], {KEY: "<key-id-1>"})
        self.assertEqual(m["account"], {"a1b2c3d4e5f6": "<account-1>"})

    def test_longest_value_first(self):
        # BUCKET は BUCKET2 の先頭部分。長い方を先に置き換えないと残骸が出る
        out = sr.redact_text(f'"{BUCKET2}" "{BUCKET}"', self.rmap())
        self.assertNotIn("synthetic", out)
        labels = self.rmap()["values"]["bucket"]
        self.assertEqual(out, f'"{labels[BUCKET2]}" "{labels[BUCKET]}"')

    def test_consistent_across_records(self):
        other = f'x {BUCKET2} y\n'
        m = sr.build_map([self.TEXT, other])
        a = sr.redact_text(self.TEXT, m)
        b = sr.redact_text(other, m)
        self.assertIn(m["values"]["bucket"][BUCKET2], a)
        self.assertEqual(b, f'x {m["values"]["bucket"][BUCKET2]} y\n')

    def test_kept_values(self):
        out = sr.redact_text(self.TEXT, self.rmap())
        for keep in ("ssh://source", "ssh://dest", "adma02bs", "r-20261104T220655Z-1341"):
            self.assertIn(keep, out)
        for gone in (HOST, BUCKET, KEY, "203.0.113.10", "a1b2c3d4e5f6", "someone"):
            self.assertNotIn(gone, out)
        self.assertIn("<home>/work", out)

    def test_machine_name_and_reserved_domain(self):
        text = "from my-test-machine to https://example.invalid/x\n"
        m = sr.build_map([text], ["my-test-machine"])
        out = sr.redact_text(text, m)
        self.assertIn("<machine>", out)
        self.assertIn("example.invalid", out)

    def test_redacted_lines_stay_json(self):
        m = self.rmap()
        for ln in self.TEXT.splitlines()[:2]:
            json.loads(sr.redact_text(ln, m))

    def test_redaction_keeps_prefix_property(self):
        m = sr.build_map([self.TEXT])
        a = req(1, [{"role": "user", "content": f"x {BUCKET}"}])
        b = req(2, [{"role": "user", "content": f"x {BUCKET}"}, msg(1)])
        shrunk = [json.loads(x) for x in sr.shrink_lines([a, b], m)]
        self.assertEqual(shrunk[1]["messages_prefix"], 1)


if __name__ == "__main__":
    unittest.main()
