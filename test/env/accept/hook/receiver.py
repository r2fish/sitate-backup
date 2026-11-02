#!/usr/bin/env python3
"""Webhook の受け手(受入試験の環境の hook。作業指示書 5.1b、3.3、GB-36、GB-103)。HTTPS(起動時に生成した自己署名の CA が署名した証明書)。

POST /hooks/<宛先>      受けた本文(イベントの JSON。spec 3.2.5)を記録して 200 を返す。宛先は URL のパスで区別する。
GET  /records            記録の全件(JSON の配列)。?dest=<宛先> で絞る。
GET  /records/<宛先>     宛先ごとの記録(JSON の配列)。
GET  /confirmation/<宛先>  その宛先で最後に受けた facts.confirmation_word({"confirmation_word": "..."})。無ければ 404。
GET  /healthz            200。
POST /_control/fail?dest=<宛先>&count=<n>   次の n 回の POST を 503 で拒否する(通知の再試行の試験用。記録には status 503 で残る)。
記録ファイル: <DATA>/records.jsonl に 1 件 1 行の JSON で追記する(欄: seq, at, dest, path, status, event_id, content_type, body または body_text, confirmation_word)。
資格情報は持たない(受け手の URL のパスが宛先の名前。試験用)。
"""
import json, os, ssl, sys, threading, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

DATA = os.environ.get("HOOK_DATA", "/var/lib/hook")
CERT = os.environ.get("HOOK_CERT", "/etc/hook/tls/server.pem")
KEY = os.environ.get("HOOK_KEY", "/etc/hook/tls/server.key")
PORT = int(os.environ.get("HOOK_PORT", "8443"))
REC = os.path.join(DATA, "records.jsonl")
lock = threading.Lock()
fail = {}  # dest -> 残りの拒否回数


def read_all():
    out = []
    try:
        with open(REC, encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if line:
                    out.append(json.loads(line))
    except FileNotFoundError:
        pass
    return out


class H(BaseHTTPRequestHandler):
    server_version = "sitate-hook/1"

    def log_message(self, fmt, *args):  # 本文と URL をログに出さない
        sys.stderr.write("%s %s\n" % (self.command, "request"))

    def _json(self, code, obj):
        b = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)

    def do_GET(self):
        u = urlparse(self.path)
        parts = [p for p in u.path.split("/") if p]
        if parts == ["healthz"]:
            return self._json(200, {"ok": True})
        if parts and parts[0] == "records":
            rows = read_all()
            dest = parts[1] if len(parts) > 1 else parse_qs(u.query).get("dest", [None])[0]
            if dest:
                rows = [r for r in rows if r["dest"] == dest]
            return self._json(200, rows)
        if len(parts) == 2 and parts[0] == "confirmation":
            for r in reversed(read_all()):
                if r["dest"] == parts[1] and r.get("confirmation_word"):
                    return self._json(200, {"confirmation_word": r["confirmation_word"]})
            return self._json(404, {"error": "not_found"})
        return self._json(404, {"error": "not_found"})

    def do_POST(self):
        u = urlparse(self.path)
        parts = [p for p in u.path.split("/") if p]
        n = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(n) if n else b""
        if len(parts) == 2 and parts[0] == "_control" and parts[1] == "fail":
            q = parse_qs(u.query)
            with lock:
                fail[q.get("dest", [""])[0]] = int(q.get("count", ["1"])[0])
            return self._json(200, {"ok": True})
        if len(parts) != 2 or parts[0] != "hooks":
            return self._json(404, {"error": "not_found"})
        dest = parts[1]
        rec = {"at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "dest": dest, "path": u.path,
               "event_id": self.headers.get("Sitate-Event-Id"), "content_type": self.headers.get("Content-Type")}
        try:
            body = json.loads(raw.decode("utf-8"))
            rec["body"] = body
            facts = body.get("facts") if isinstance(body, dict) else None
            rec["confirmation_word"] = facts.get("confirmation_word") if isinstance(facts, dict) else None
        except Exception:
            rec["body_text"] = raw.decode("utf-8", "replace")
            rec["confirmation_word"] = None
        with lock:
            status = 200
            if fail.get(dest, 0) > 0:
                fail[dest] -= 1
                status = 503
            rec["status"] = status
            rec["seq"] = len(read_all()) + 1
            os.makedirs(DATA, exist_ok=True)
            with open(REC, "a", encoding="utf-8") as f:
                f.write(json.dumps(rec, ensure_ascii=False) + "\n")
            os.chmod(REC, 0o644)
        return self._json(status, {"ok": status == 200})


def main():
    srv = ThreadingHTTPServer(("0.0.0.0", PORT), H)
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(CERT, KEY)
    srv.socket = ctx.wrap_socket(srv.socket, server_side=True)
    sys.stderr.write("hook listening %d\n" % PORT)
    srv.serve_forever()


if __name__ == "__main__":
    main()
