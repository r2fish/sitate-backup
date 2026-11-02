// 接続先の登録・削除と指紋の入力(spec 3.7)。固定済みの指紋は上書きせず、形の外れた入力は何も書かない。
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { TargetActions } from "../../src/main/target-actions.ts";
import { TargetStore } from "../../src/main/ssh/target-store.ts";

const FP1 = "SHA256:" + "A".repeat(43);
const FP2 = "SHA256:" + "B".repeat(43);

function rig() {
  const dir = mkdtempSync(join(tmpdir(), "u3a-tgt-"));
  const targets = new TargetStore(join(dir, "t.json"));
  const typedFp = { current: null as string | null };
  let changes = 0;
  const a = new TargetActions({ targets, typedFp, label: (k) => k, onChange: () => { changes++; } });
  return { a, targets, typedFp, changes: () => changes, done: () => rmSync(dir, { recursive: true, force: true }) };
}

test("登録は形を確かめて書く。指紋があれば固定し、既に有れば上書きしない", () => {
  const r = rig();
  try {
    assert.deepEqual(r.a.register({ host: "backup.example.test", port: "22", fingerprint: FP1 }), { ok: true });
    assert.deepEqual(r.targets.read(), { host: "backup.example.test", port: 22, user: "sitate", hostKeyFp: FP1, adminPublicKey: null });
    assert.deepEqual(r.a.register({ host: "other.example.test", port: 22 }), { ok: false, reason: "exists" });
    assert.equal(r.targets.read()!.host, "backup.example.test");
    assert.deepEqual(r.a.setFingerprint(FP2), { ok: false, reason: "exists" });
    assert.equal(r.targets.read()!.hostKeyFp, FP1);
    assert.equal(r.a.view().fingerprintPinned, true);
    assert.equal(r.typedFp.current, FP1, "登録時に入れた指紋は bootstrap の照合のために保持される");
  } finally { r.done(); }
});

test("形の外れたホスト・ポート・指紋は何も書かず、固定文の鍵を出す", () => {
  const r = rig();
  try {
    for (const host of ["", "-oProxyCommand=x", "-oProxyCommand", "-x.example.test", "-", "a b", "a;b", "a/b", "a\nb"]) assert.deepEqual(r.a.register({ host, port: 22 }), { ok: false, reason: "host" }, host);
    for (const port of ["0", "65536", "ab", "22 ", "-1", 22.5]) assert.deepEqual(r.a.register({ host: "h.example.test", port }), port === "22 " ? { ok: true } : { ok: false, reason: "port" }, String(port));
    r.a.remove();
    assert.deepEqual(r.a.register({ host: "h.example.test", port: 22, fingerprint: "SHA256:short" }), { ok: false, reason: "fingerprint" });
    assert.equal(r.targets.read(), null);
    assert.equal(r.a.view().notice, "status.target.invalid.fingerprint");
  } finally { r.done(); }
});

test("指紋は登録前に入れると保持され、登録済みで未固定なら固定され、削除で固定値も保持も捨てる", () => {
  const r = rig();
  try {
    assert.deepEqual(r.a.setFingerprint("  " + FP1 + " "), { ok: true });
    assert.equal(r.typedFp.current, FP1);
    r.a.register({ host: "h.example.test", port: 2222 });
    assert.equal(r.targets.read()!.hostKeyFp, null);
    assert.deepEqual(r.a.setFingerprint(FP2), { ok: true });
    assert.equal(r.targets.read()!.hostKeyFp, FP2);
    assert.deepEqual(r.a.remove(), { ok: true });
    assert.equal(r.targets.read(), null);
    assert.equal(r.typedFp.current, null);
    assert.deepEqual(r.a.remove(), { ok: false, reason: "none" });
    assert.ok(r.changes() === 5);
  } finally { r.done(); }
});
