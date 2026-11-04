// システムプロンプトが問いの出し方の指示を含む(spec 3.7、作業指示書 5.14 の追加の完了条件。D-0171)。
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { buildSystemBlocks } from "../../src/agent/system-prompt.ts";

const root = join(import.meta.dirname, "..", "..", "..");
const read = (...p: string[]): string => readFileSync(join(root, ...p), "utf8");
const rulesDir = join(root, "assets", "rules");
const rules = readdirSync(rulesDir).sort().map((f) => readFileSync(join(rulesDir, f), "utf8")).join("\n\n");

test("[D-0171] システムプロンプトは、疑問符で終える・1 回に 1 つ・最後の文にする・手順の順を守る、を指示する", () => {
  const blocks = buildSystemBlocks({ manual: "m", procedure: read("docs", "install.md"), rules, tools: [], models: ["m"] }, { locale: "ja", date: "2026-11-03", model: "m" });
  const text = blocks.map((b) => b.text).join("\n");
  for (const must of ["問いは疑問符(「?」)で終える", "1 回の発話に問いは 1 つだけ", "問いは発話の最後の文にする", "問いの後に文を続けない", "Q3 は手順 3、Q4 は手順 4 の前半", "開始時の入力", "道具を呼ばない", "守りたいものを聞く問い(Q4)を、案の前の発話の最後に置く", "人の答えを受けてから定義の案を作る", "答えを待たずに「指定が無いので」と案の確認を求めない", "人が分からないと答えたときだけ"]) assert.ok(text.includes(must), must);
  assert.ok(blocks[2]!.text.includes("問いの出し方"), "規則ファイルの節(キャッシュの区切りの前)に入っている");
});
