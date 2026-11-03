// mp-app C2: 自前の BLAKE2b-512 は Node の OpenSSL の実装と一致する。Electron の Node は blake2b512 を持たず、署名の検証が落ちた(煙試験で発覚)。
import assert from "node:assert/strict";
import { createHash, randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { blake2b512 } from "../../src/main/blake2b.ts";

test("[mp-app C2] blake2b512 は境界の長さと乱数で OpenSSL の実装と一致する", () => {
  assert.equal(blake2b512(Buffer.alloc(0)).toString("hex"), createHash("blake2b512").update("").digest("hex"));
  for (const n of [1, 3, 127, 128, 129, 255, 256, 257, 1000, 65537]) {
    const b = randomBytes(n);
    assert.equal(blake2b512(b).toString("hex"), createHash("blake2b512").update(b).digest("hex"), String(n));
  }
});

test("[mp-app C2] minisign の検証は createHash('blake2b512') に頼らない", () => {
  assert.doesNotMatch(readFileSync(join(import.meta.dirname, "..", "..", "src", "main", "minisign.ts"), "utf8"), /createHash\("blake2b512"\)/);
});
