// 核の標準出力の受け口(spec 3.1.4、3.7、完了条件 6)。先頭が `sitate.secret-string/v1` なら秘密をフレーズのパネルへ直接渡し、
// モデルへ渡すのは封筒だけ。破棄する出力は秘密を捨て、アプリの原因コードの封筒を返す。秘密はここから他へ出ない(戻り値・例外・ログに載せない)。
import { appErrorEnvelope } from "./app-envelope.ts";
import type { AppEnvelope } from "./app-envelope.ts";
import { PhrasePanel } from "./phrase-panel.ts";
import { parseStdoutRecords } from "./stdout-records.ts";
import type { OutEnvelope, StreamEnd } from "./stdout-records.ts";

export type Ingested =
  | { readonly kind: "envelope"; readonly envelope: OutEnvelope; readonly panel: PhrasePanel | null }
  | { readonly kind: "discarded"; readonly envelope: AppEnvelope };

export function ingestCoreOutput(stdout: string, command: readonly string[], end: StreamEnd = "complete"): Ingested {
  const r = parseStdoutRecords(stdout, end);
  if (!r.ok) return { kind: "discarded", envelope: appErrorEnvelope(r.code, command) };
  return { kind: "envelope", envelope: r.envelope, panel: r.secret === null ? null : new PhrasePanel(r.secret) };
}
