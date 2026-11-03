// 予行(b2-preflight.sh)の記録 record.jsonl から、各道具の合否と、bucket check の試しの結果 permissions[](op・key_role・expected・actual だけ)を出す。
// 値(鍵・秘密)は読まない・出さない。使い方: node test/wiring/preflight-summary.ts <record.jsonl>
import { readFileSync } from "node:fs";
import { parseEnvelope } from "../../app/test/e2e/envelope.ts";

const WATCH = ["sitate_init_storage", "sitate_bucket_check", "sitate_backup", "sitate_extend", "sitate_restore_test"];
const file = process.argv[2];
if (file === undefined) { console.error("使い方: node test/wiring/preflight-summary.ts <record.jsonl>"); process.exit(64); }
const all = readFileSync(file, "utf8").split("\n").filter((l) => l !== "").map((l) => JSON.parse(l) as { kind: string; tool?: string; envelope?: string });
const lastHeader = all.map((r) => r.kind).lastIndexOf("header"); // 最後の回だけを読む(追記された前の回を混ぜない)
const rows = lastHeader < 0 ? all : all.slice(lastHeader);
let bad = 0;
const seen = new Set<string>();
for (const r of rows) {
  if (r.kind !== "tool_call" || r.tool === undefined || !WATCH.includes(r.tool)) continue;
  const e = parseEnvelope(String(r.envelope));
  console.log(`${r.tool}: ok=${String(e.ok)}${e.code === null ? "" : ` code=${e.code}`}`);
  seen.add(r.tool);
  if (e.ok !== true) bad += 1;
  const perms = r.tool === "sitate_bucket_check" ? e.result?.permissions : undefined;
  if (Array.isArray(perms)) {
    for (const p of perms as { op?: string; key_role?: string; expected?: string; actual?: string }[]) console.log(`  permissions: op=${p.op} key_role=${p.key_role} expected=${p.expected} actual=${p.actual}${p.expected !== p.actual ? "  <-- 不一致" : ""}`);
  }
}
for (const t of WATCH) if (!seen.has(t)) { console.log(`${t}: 呼ばれていない`); bad += 1; }
const summary = rows.filter((r) => r.kind === "summary").at(-1) as { exit?: number; reason?: string } | undefined;
console.log(`driver: 終了コード ${String(summary?.exit)}: ${String(summary?.reason)}`);
process.exitCode = bad > 0 || summary?.exit !== 0 ? 1 : 0;
