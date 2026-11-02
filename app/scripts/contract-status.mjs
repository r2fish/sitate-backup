// app/test/contract/ と U3a・U3b の試験の結果を 1 件 1 行の JSON({wp, status, out})で標準出力に出す。
// make contract-status が読む。割り当て: 名前に [u3b] を含む試験と src/kit/ の試験は U3b、それ以外は U3a(作業指示書 3.1)。
import { globSync } from "node:fs";
import { run } from "node:test";

const files = ["test/contract/*.test.ts", "test/u3a/*.test.ts", "src/kit/*.test.ts"].flatMap((p) => globSync(p));
const stream = run({ files, concurrency: false });
for await (const ev of stream) {
  if (!["test:pass", "test:fail"].includes(ev.type) && ev.type !== "test:skip") continue;
  const d = ev.data;
  if (d.nesting !== 0 && d.details?.type === "suite") continue;
  const wp = d.name.includes("[u3b]") || (d.file ?? "").includes("/src/kit/") ? "U3b" : "U3a";
  const status = ev.type === "test:pass" ? "pass" : ev.type === "test:fail" ? "fail" : "skip";
  const out = String(d.details?.error?.message ?? d.details?.error?.cause?.message ?? d.skip ?? "");
  console.log(JSON.stringify({ wp, status, out }));
}
