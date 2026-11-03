// 配布署名(minisign 互換。spec 3.2.1「配布署名の形」)の検証。方式は既定の `ED`(対象の BLAKE2b-512 に Ed25519)だけを受ける。
// trusted comment(`version=<版> arch=<arch> path=<キー>`)も署名の対象で、得た位置と照らす。
import { createPublicKey, verify } from "node:crypto";
import { blake2b512 } from "./blake2b.ts";

const SPKI_PREFIX = Buffer.from("302a300506032b6570032100", "hex");
const edVerify = (rawKey: Buffer, msg: Buffer, sig: Buffer): boolean =>
  verify(null, msg, createPublicKey({ key: Buffer.concat([SPKI_PREFIX, rawKey]), format: "der", type: "spki" }), sig);

export interface Placement { readonly version: string; readonly arch: string; readonly path: string }
export interface TrustedKey { readonly keyId: Buffer; readonly publicKey: Buffer }

/** minisign の公開鍵 1 行(`Ed` + 鍵 ID 8 + 公開鍵 32 の 42 バイトの base64)。 */
export function parsePublicKey(line: string): TrustedKey {
  const b = Buffer.from(line.trim(), "base64");
  if (b.length !== 42 || b.subarray(0, 2).toString("latin1") !== "Ed") throw new Error("公開鍵の形が不正");
  return { keyId: b.subarray(2, 10), publicKey: b.subarray(10, 42) };
}

export type VerifyFailure = "no_matching_key" | "bad_signature" | "bad_format" | "placement_mismatch" | "unsupported_algorithm";

export function verifyMinisign(file: Buffer, sigText: string, keys: readonly TrustedKey[], expected: Placement): { ok: true } | { ok: false; reason: VerifyFailure } {
  const lines = sigText.split("\n").map((l) => l.replace(/\r$/, ""));
  const sigLine = lines[1];
  const trustedLine = lines[2];
  const globalLine = lines[3];
  if (lines[0] === undefined || !lines[0].startsWith("untrusted comment:") || sigLine === undefined || trustedLine === undefined || !trustedLine.startsWith("trusted comment: ") || globalLine === undefined) return { ok: false, reason: "bad_format" };
  const raw = Buffer.from(sigLine, "base64");
  if (raw.length !== 74) return { ok: false, reason: "bad_format" };
  if (raw.subarray(0, 2).toString("latin1") !== "ED") return { ok: false, reason: "unsupported_algorithm" };
  const key = keys.find((k) => k.keyId.equals(raw.subarray(2, 10)));
  if (key === undefined) return { ok: false, reason: "no_matching_key" };
  const sig = raw.subarray(10, 74);
  const digest = blake2b512(file);
  if (!edVerify(key.publicKey, digest, sig)) return { ok: false, reason: "bad_signature" };
  const trusted = trustedLine.slice("trusted comment: ".length);
  const global = Buffer.from(globalLine, "base64");
  if (global.length !== 64 || !edVerify(key.publicKey, Buffer.concat([sig, Buffer.from(trusted, "utf8")]), global)) return { ok: false, reason: "bad_signature" };
  if (trusted !== `version=${expected.version} arch=${expected.arch} path=${expected.path}`) return { ok: false, reason: "placement_mismatch" };
  return { ok: true };
}
