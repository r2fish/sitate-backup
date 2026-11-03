// mp-app B4・中: 画面に出す文では引用の印を人に読める書式にする。手順書の値を引いた文は、手順書を引用元として表示できる。
import assert from "node:assert/strict";
import { test } from "node:test";
import { renderMarks } from "../../src/main/quote-render.ts";
import { env, harness } from "./mp-helpers.ts";

const t = (k: string): string => ({ "card.value.true": "はい", "card.value.false": "いいえ" } as Record<string, string>)[k] ?? k;

test("[mp-app B4] 印は値だけの読める書式に置き換わる(日付・大きさ・金額・真偽・文字列)", () => {
  const out = renderMarks("最後は [[status:/result/last_backup/at=2026-11-03T01:00:00Z]]、[[estimate:/result/monthly_jpy=1200]]、[[status:/result/initialized=true]]、[[version:/result/signing_key_fp=SHA256:abc]]。", t, "Asia/Tokyo");
  assert.ok(!out.includes("[["), out);
  assert.match(out, /2026年11月3日 10:00/);
  assert.match(out, /1,200 円/);
  assert.match(out, /はい/);
  assert.match(out, /SHA256:abc/);
});

test("[mp-app B4] 会話では、照合を通った文が印なしで表示される", async () => {
  const h = harness((argv) => env([argv[0]!], { initialized: true }));
  await h.s.onToolCall("sitate_status", {});
  h.s.onReply("初期化は [[status:/result/initialized=true]] です。");
  const shown = h.lines.filter((l) => l.role === "agent").map((l) => l.text).join("\n");
  assert.ok(shown.length > 0 && !shown.includes("[["), shown);
});

test("[mp-app 中] 手順書の本文に出る値を引いた文は、引用元の指定で表示される", async () => {
  const h = harness((argv) => env([argv[0]!], {}), { kind: "close" }, "x", { citedBodies: () => ["手順 13 の期待は COMPLIANCE 以外を許さない。"] });
  h.s.onReply("保管先の設定の値は COMPLIANCE になっていることを確かめます。");
  assert.equal(h.lines.filter((l) => l.role === "agent").length, 1);
  assert.equal(h.lines.filter((l) => l.role === "notice").length, 0);
});
