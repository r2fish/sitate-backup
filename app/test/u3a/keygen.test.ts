import assert from "node:assert/strict";
import { test } from "node:test";
import ssh2 from "ssh2";
import { generateReadableKeyPair } from "../../src/main/ssh/keygen.ts";

const { utils } = ssh2;

test("keygen: an unreadable key is replaced by a readable one", () => {
  const good = utils.generateKeyPairSync("ed25519");
  let calls = 0;
  const k = generateReadableKeyPair(() => (++calls === 1 ? { private: "malformed", public: "malformed" } : good));
  assert.equal(calls, 2);
  assert.equal(k.private, good.private);
});

test("keygen: gives up after the attempts run out", () => {
  assert.throws(() => generateReadableKeyPair(() => ({ private: "malformed", public: "malformed" })), /KEYGEN_FAILED/);
});
