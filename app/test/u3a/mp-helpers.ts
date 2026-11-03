// mp-app の試験の共通部品: 会話の制御(Session)を、記録する偽の核・偽の入力ボックスで組む。
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { SoftwareApprovalKey } from "../../src/main/approval-sign.ts";
import type { CardModel } from "../../src/main/approval-card.ts";
import type { CardDecision } from "../../src/main/ipc-channels.ts";
import { MemoryKeychain, serviceFor } from "../../src/main/keychain.ts";
import { PathRefTable } from "../../src/main/path-ref.ts";
import { SecretBox } from "../../src/main/secret-box.ts";
import type { SecretRequest } from "../../src/main/secret-box.ts";
import { Session } from "../../src/main/session.ts";
import type { ChatLine, CoreRunner, SessionDeps } from "../../src/main/session.ts";
import { ToolRegistry } from "../../src/main/tool-registry.ts";

const keys = JSON.parse(readFileSync(new URL("../../src/i18n/keys.json", import.meta.url), "utf8")) as Record<string, unknown>;
export const t = (key: string, args?: Record<string, unknown>): string => {
  if (!Object.hasOwn(keys, key)) throw new Error("no key " + key);
  return args === undefined ? key : key + JSON.stringify(args);
};
export const registry = ToolRegistry.fromDir(join(import.meta.dirname, "..", "..", "src", "tools"));
export const env = (command: string[], result: unknown, ok = true): string => JSON.stringify({ schema: "sitate.out/v1", command, ok, dry_run: false, run_id: null, warnings: [], result, ...(ok ? {} : { error: { code: "X", message: "m", detail: {} } }) }) + "\n";

export type Reply = (argv: readonly string[], stdin: string) => string;

export interface Harness {
  readonly s: Session;
  readonly calls: { argv: readonly string[]; stdin: string }[];
  readonly key: SoftwareApprovalKey;
  readonly lines: ChatLine[];
  readonly cards: CardModel[];
  readonly kc: MemoryKeychain;
  readonly boxes: SecretRequest[];
  readonly box: SecretBox;
}

/** reply が argv から標準出力(封筒)を返す。decision は承認カードへの人の操作。 */
export type Decide = CardDecision | ((c: CardModel) => Promise<CardDecision>);
export function harness(reply: Reply, decision: Decide = { kind: "close" }, secretValue = "SECRET-VALUE", extra: Partial<SessionDeps> = {}): Harness {
  const calls: Harness["calls"] = [];
  const runner: CoreRunner = { async run(argv, stdin) { calls.push({ argv, stdin }); return { stdout: reply(argv, stdin), end: "complete" }; } };
  const key = SoftwareApprovalKey.generate();
  const lines: ChatLine[] = [];
  const cards: CardModel[] = [];
  const kc = new MemoryKeychain();
  const boxes: SecretRequest[] = [];
  const box: SecretBox = new SecretBox({ keychain: kc, service: () => serviceFor("repo"), show: (r) => { if (r !== null) { boxes.push(r); setImmediate(() => void box.submit(r.ref, secretValue)); } } });
  const s = new Session({
    registry, refs: new PathRefTable(), keychain: kc, service: () => serviceFor("repo"), runner, t, approvalKey: key,
    now: () => "2026-11-03T01:05:00Z", sendToAgent: () => undefined, onChat: (l) => lines.push(l),
    onCard: async (c) => { cards.push(c); return typeof decision === "function" ? decision(c) : decision; }, onPanel: async () => "done" as const, secretBox: box, ...extra,
  });
  return { s, calls, key, lines, cards, kc, boxes, box };
}
