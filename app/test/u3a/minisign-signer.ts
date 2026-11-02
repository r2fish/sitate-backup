// 試験用の minisign 署名者(使い捨ての鍵)。trusted comment は `version=<版> arch=<arch> path=<キー>`。
import { createHash, generateKeyPairSync, randomBytes, sign } from "node:crypto";

export interface TestSigner { readonly pub: string; signFile(bytes: Buffer, path: string, version: string, arch: string): string }

export function newSigner(): TestSigner {
  const { privateKey, publicKey } = generateKeyPairSync("ed25519");
  const keyId = randomBytes(8);
  const raw = publicKey.export({ type: "spki", format: "der" }).subarray(-32);
  return {
    pub: Buffer.concat([Buffer.from("Ed"), keyId, raw]).toString("base64"),
    signFile(bytes, path, version, arch) {
      const sig = sign(null, createHash("blake2b512").update(bytes).digest(), privateKey);
      const trusted = `version=${version} arch=${arch} path=${path}`;
      const global = sign(null, Buffer.concat([sig, Buffer.from(trusted)]), privateKey);
      return `untrusted comment: test\n${Buffer.concat([Buffer.from("ED"), keyId, sig]).toString("base64")}\ntrusted comment: ${trusted}\n${global.toString("base64")}\n`;
    },
  };
}
