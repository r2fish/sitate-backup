// 信頼の起点(完了条件 13)。ビルド(scripts/build-app.mjs)が `embedded-keys.json` をここへ書く。製品のアプリは配布用の公開鍵だけ、
// 試験用のアプリは使い捨ての鍵だけを持ち、両方を信頼するビルドは作らない。公開鍵は minisign の 1 行(最大 2 つ)。
import { parsePublicKey } from "../main/minisign.ts";
import type { TrustedKey } from "../main/minisign.ts";

export interface EmbeddedKeys { readonly kind: "product" | "test"; readonly keys: readonly string[] }

export function trustedKeysOf(e: EmbeddedKeys): TrustedKey[] {
  if (e.keys.length < 1 || e.keys.length > 2) throw new Error("埋め込む公開鍵は 1〜2 個");
  return e.keys.map(parsePublicKey);
}
