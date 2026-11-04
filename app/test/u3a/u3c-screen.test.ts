// U3c 画面の洗練(5.14c の完了条件 1・3・4・5 の画面側): 試験用ドライバが読む DOM の互換、配置と配色、道具の行の畳みと導入の進み具合、応答の待ちの印。
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { uiChatLines } from "../../src/main/chat-view.ts";
import type { ChatLine } from "../../src/main/session.ts";
import { scriptFor } from "../../src/testing/driver-server.ts";
import { NEW_HTML, NEW_RENDERER, OLD_HTML, OLD_RENDERER, appRoot, mount, words } from "./u3c-screen-helpers.ts";

const OLD_LINES = `[...document.querySelectorAll('#chat-lines li')].map((li)=>({role:li.getAttribute('data-role'),text:li.textContent}))`;
const heading = (n: number): string => `H${n}`;
const L = (role: ChatLine["role"], text: string, extra: Partial<ChatLine> = {}): ChatLine => ({ role, text, ...extra });

const CHAT: ChatLine[] = [
  L("human", "始めてください"), L("agent", "# 見出し\n\n**太字** と `code` と\n- 一つ\n- 二つ\n\n```\nraw <b>x</b>\n```"),
  L("tool", "サーバーの点検: 成功"), L("tool", "保管先の確認: 成功"), L("tool", "手順 1 が合格しました。", { step: true }),
  L("agent", "終わりました"), L("notice", "固定文"), L("tool", "状態の読み取り: 成功"),
];
const chatMsg = (lines: readonly ChatLine[], wait: string | null = null, uiLines = true): unknown => ({
  surface: "chat", sendLabel: "送る", start: null, wait,
  lines: uiLines ? uiChatLines(lines, heading) : lines.map((l) => ({ role: l.role, text: l.text })),
});
const card = { changeId: "c-1", approveEnabled: true, updatePrompt: null, proofRecipients: ["mail", "hook"], proofLabels: { mail: "メールの語", hook: "通知の語" }, labels: { approve: "承認する", cancel: "取り消す", close: "閉じる" },
  sections: [{ no: 1, title: "操作", rows: [{ label: "操作の名前", value: "保管庫を作る" }, { label: "月額", value: "1,200 円" }], lines: ["AI の説明: これは説明です"] }, { no: 2, title: "確認", rows: [{ label: "期限", value: "11 月 10 日" }], lines: [] }] };
const secret = { surface: "secret-box", mode: "save", ref: "r1", label: "管理用の資格情報を入れてください", submitLabel: "保存" };
const phrase = { surface: "phrase-panel", open: true, words: [{ n: 1, word: "abandon" }, { n: 2, word: "ability" }, { n: 3, word: "able" }], canProceed: false, actions: [{ action: "verify", label: "確かめる" }, { action: "cancel", label: "やめる" }], notice: "語を書き写してください", cautions: ["写真に撮らない"] };

async function observe(renderer: string, html: string, linesScript: string, ui: boolean): Promise<{ lines: unknown; pending: Record<string, unknown>[] }> {
  const m = await mount(renderer, html);
  m.render(chatMsg(CHAT, null, ui));
  const lines = m.run(linesScript);
  const pending: Record<string, unknown>[] = [];
  for (const msg of [{ surface: "approval-card", card }, secret, phrase]) {
    m.render(msg);
    pending.push(m.run(scriptFor({ op: "pending" })) as Record<string, unknown>);
  }
  return { lines, pending };
}

test("[U3c-1] 試験用ドライバの lines と pending は直す前の画面と同じ(会話・カード・秘密の入力ボックス・フレーズのパネル)", async () => {
  const before = await observe(OLD_RENDERER, OLD_HTML, OLD_LINES, false);
  const after = await observe(NEW_RENDERER, NEW_HTML, scriptFor({ op: "lines" }), true);
  assert.deepEqual(after.lines, before.lines, "lines は行の列が同じ");
  assert.equal((after.lines as unknown[]).length, CHAT.length);
  const norm = (p: Record<string, unknown>): unknown => {
    const c = p.card as { text: string } | null;
    return { ...p, card: c === null ? null : { ...c, text: words(c.text) } };
  };
  assert.deepEqual(after.pending.map(norm), before.pending.map(norm), "pending は text の中の行の順と語が同じ(ほかの欄は完全に同じ)");
  const cardPending = after.pending[0]!.card as { text: string; proofs: string[]; actions: unknown[] };
  assert.ok(cardPending.text.includes("保管庫を作る") && cardPending.proofs.length === 2 && cardPending.actions.length === 3, "操作の名前は #card-body の中");
  assert.equal(after.pending[1]!.secret, "管理用の資格情報を入れてください", "#secret-label の文は送った文と完全に同じ");
  assert.deepEqual((after.pending[2]!.phrase as { words: string[] }).words, ["abandon", "ability", "able"], "語の li は語だけ");
});

test("[U3c-1] 会話が 1 行ずつ伸びても lines は 1 ずつ増え、前の行は変わらない(畳みの見出しを足しても数は変わらない)", async () => {
  const m = await mount(NEW_RENDERER, NEW_HTML);
  let prev: { role: string; text: string }[] = [];
  CHAT.forEach((_l, i) => {
    m.render(chatMsg(CHAT.slice(0, i + 1)));
    const now = m.run(scriptFor({ op: "lines" })) as { role: string; text: string }[];
    assert.equal(now.length, i + 1);
    assert.deepEqual(now.slice(0, prev.length), prev);
    assert.deepEqual(now[i], { role: CHAT[i]!.role, text: CHAT[i]!.text });
    prev = now;
  });
  const list = m.doc.getElementById("chat-lines")!;
  assert.ok(list.children.every((c) => typeof c !== "string" && c.tag === "li"), "#chat-lines の直下は li だけ");
});

test("[U3c-3] 配置と配色: 中央のチャット欄・右の状態の欄・中央のダイアログ・明暗。外部の資源を読まず CSP は変わらない", () => {
  const css = readFileSync(join(appRoot, "src/renderer/style.css"), "utf8");
  const html = readFileSync(NEW_HTML, "utf8");
  assert.match(css, /prefers-color-scheme:\s*dark/);
  assert.match(css, /#chat-column\s*\{[^}]*max-width:\s*760px[^}]*margin:\s*0 auto/);
  assert.match(css, /main\s*\{[^}]*grid-template-columns:\s*minmax\(0, 1fr\) 300px/);
  assert.match(css, /\[data-surface="status"\]\s*\{[^}]*grid-column:\s*2/);
  assert.match(css, /dialog\s*\{[^}]*width:\s*560px[^}]*border-radius:\s*18px/);
  const rule = (sel: string): string => new RegExp(`${sel.replace(/[[\]().>"]/g, "\\$&")}\\s*\\{([^}]*)\\}`).exec(css)?.[1] ?? "";
  assert.match(rule('#chat-lines > li[data-role="human"]'), /align-self:\s*flex-end/, "人の発話は右寄せの吹き出し");
  assert.match(rule('#chat-lines > li[data-role="human"]'), /background/);
  assert.doesNotMatch(rule('#chat-lines > li[data-role="agent"]'), /background|border/, "エージェントの文は吹き出し無し");
  assert.match(rule('#chat-lines > li[data-role="notice"]'), /border-left:\s*3px solid/, "知らせは左の線");
  assert.match(html, /<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'self'; style-src 'self'">/);
  for (const [name, text] of [["css", css], ["html", html]] as const) {
    assert.doesNotMatch(text.replace(/<!doctype html>/i, ""), /https?:|\/\/|data:|url\(|@import/i, `${name} が外部の URL・data: を読まない`);
  }
  assert.match(html, /<dialog data-surface="approval-card"/);
  assert.match(html, /<dialog data-surface="secret-box"/);
  assert.match(html, /<dialog data-surface="phrase-panel"/);
});

test("[U3c-3] 状態の行・調査の結果の行・承認カードの行は、欄(ラベルと値)ごとに要素を分けて描く", async () => {
  const m = await mount(NEW_RENDERER, NEW_HTML);
  m.render({ surface: "status", heading: "状態", rows: [{ label: "最後のバックアップ", value: "11/3 ✓" }, { label: "見出しだけ", value: "" }], askAi: [], steps: null,
    target: { registered: false, host: null, port: null, fingerprintPinned: false, notice: null, labels: { heading: "接続先", host: "h", port: "p", fingerprint: "f", register: "r", remove: "x", pinned: "pin" } },
    survey: { heading: "調査", rows: [{ name: "/srv/a", kind: "ディレクトリ", size: "1 GiB", v1: "対応", v1ok: true }, { name: "/srv/b", kind: "マウント", size: "—", v1: "対象外", v1ok: false }] } });
  const rows = m.doc.getElementById("status-rows")!.querySelectorAll("li");
  assert.deepEqual(rows[0]!.children.map((c) => (typeof c === "string" ? c : `${c.getAttribute("class")}=${c.textContent}`)), ["f-label=最後のバックアップ", "f-value=11/3 ✓"]);
  assert.equal(rows[1]!.children.length, 1, "値が空ならラベルだけ");
  const survey = m.doc.querySelectorAll("[data-survey-row]");
  assert.equal(survey.length, 2);
  assert.deepEqual(survey[1]!.children.map((c) => (typeof c === "string" ? "" : c.getAttribute("class"))), ["s-name", "s-kind", "s-size", "tag tag-ng"]);
  m.render({ surface: "approval-card", card });
  const crow = m.doc.querySelectorAll("#card-body [data-card-row]");
  assert.equal(crow.length, 3);
  assert.deepEqual(crow[0]!.children.map((c) => (typeof c === "string" ? "" : c.textContent)), ["操作の名前", "保管庫を作る"]);
});

const toolRuns: ChatLine[] = [L("human", "q"), L("tool", "a"), L("tool", "b"), L("tool", "c", { step: true }), L("agent", "ans"), L("tool", "d"), L("tool", "e")];

test("[U3c-4] 道具の行の畳み: 見出しは並びの最初の行の li の中。最後の並びは開き、前の並びは畳み、人が開いた並びは描き直しても開いたまま", async () => {
  const m = await mount(NEW_RENDERER, NEW_HTML);
  m.render(chatMsg(toolRuns));
  const lis = () => m.doc.getElementById("chat-lines")!.children as import("./fake-dom.ts").FakeEl[];
  assert.equal(lis().length, toolRuns.length, "li の数は行の数と同じ");
  const heads = m.doc.querySelectorAll("button.fold-head");
  assert.deepEqual(heads.map((h) => h.textContent), ["H2 ▸", "H2 ▾"], "件数は手順の合格の行を数えない(2 回)");
  assert.equal(heads[0]!.parent, lis()[1], "見出しは並びの最初の行の要素の中");
  assert.deepEqual(lis().map((l) => l.hidden), [false, false, true, true, false, false, false], "最初の並びは 2 つ目以降が隠れ、最後の並びは開いたまま");
  heads[0]!.click();
  assert.deepEqual(lis().map((l) => l.hidden), [false, false, false, false, false, false, false]);
  m.render(chatMsg(toolRuns));
  assert.deepEqual(lis().map((l) => l.hidden), [false, false, false, false, false, false, false], "描き直しても人が開いた並びは開いたまま");
  m.render(chatMsg([...toolRuns, L("agent", "あとの文")]));
  assert.deepEqual(lis().map((l) => l.hidden), [false, false, false, false, false, false, true, false], "後にエージェントの文が来たら最後だった並びは畳む(開いた並びはそのまま)");
  assert.deepEqual(m.run(scriptFor({ op: "lines" })), [...toolRuns, L("agent", "あとの文")].map((l) => ({ role: l.role, text: l.text })), "行の文と順は変わらない");
});

test("[U3c-4] 導入の進み具合: 手順の一覧に合格の印と「次」の印。一覧が無い(null)ときは出さない", async () => {
  const m = await mount(NEW_RENDERER, NEW_HTML);
  const status = (steps: unknown): unknown => ({ surface: "status", heading: "h", rows: [], askAi: [], survey: null, steps, target: { registered: false, host: null, port: null, fingerprintPinned: false, notice: null, labels: { heading: "", host: "", port: "", fingerprint: "", register: "", remove: "", pinned: "" } } });
  m.render(status(null));
  assert.equal(m.doc.getElementById("status-steps")!.hidden, true);
  assert.equal(m.doc.querySelectorAll("#status-steps li").length, 0);
  assert.equal(m.doc.getElementById("status-progress")!.hidden, true);
  m.render(status({ heading: "導入の進み具合", nextLabel: "次", rows: [{ n: 1, label: "導入", state: "passed" }, { n: 2, label: "保管先", state: "next" }, { n: 3, label: "資格情報", state: "todo" }] }));
  assert.equal(m.doc.getElementById("status-steps")!.hidden, false);
  assert.equal(m.doc.getElementById("status-steps-heading")!.textContent, "導入の進み具合", "一覧の上の見出し");
  assert.deepEqual(m.doc.querySelectorAll("#status-steps .step-tag").map((e) => e.textContent), ["次"], "「次」は文字の札(次の手順だけ)");
  assert.equal(m.doc.querySelectorAll("#status-steps li")[1]!.getAttribute("data-state"), "next");
  assert.deepEqual(m.doc.querySelectorAll("#status-steps li").map((l) => l.getAttribute("data-state")), ["passed", "next", "todo"]);
  assert.equal(m.doc.querySelectorAll("#status-steps li")[0]!.textContent, "✓導入");
  assert.equal(m.doc.querySelectorAll("#status-steps li")[1]!.textContent, "→保管先次");
  m.render(status(null));
  assert.equal(m.doc.querySelectorAll("#status-steps li").length, 0, "会話を捨てると消える");
});

test("[U3c-5] 応答の待ちの印は #chat-lines の外にあり、主プロセスが送った文を出し、送らなければ隠れる。行の数に数えない", async () => {
  const m = await mount(NEW_RENDERER, NEW_HTML);
  m.render(chatMsg([L("human", "q")], "応答を待っています"));
  const wait = m.doc.getElementById("chat-wait")!;
  assert.equal(wait.hidden, false);
  assert.equal(m.doc.getElementById("chat-wait-label")!.textContent, "応答を待っています");
  assert.ok(!m.doc.getElementById("chat-lines")!.descendants().includes(wait), "一覧の外");
  assert.equal(m.doc.getElementById("chat-lines")!.children.length, 1);
  m.render(chatMsg([L("human", "q")], null));
  assert.equal(wait.hidden, true);
});

test("[U3c-3] 送信ボタン: 文はカタログ(主プロセスが送る)、aria-label も同じ文。文字は CSS の ↑", async () => {
  const m = await mount(NEW_RENDERER, NEW_HTML);
  m.render(chatMsg([]));
  assert.equal(m.doc.getElementById("chat-send")!.textContent, "送る");
  assert.equal(m.doc.getElementById("chat-send")!.getAttribute("aria-label"), "送る");
  assert.match(readFileSync(join(appRoot, "src/renderer/style.css"), "utf8"), /#chat-send::after\s*\{\s*content:\s*"↑"/);
});

test("[U3c-2] 静的検査: app/src/renderer/ に innerHTML・outerHTML・insertAdjacentHTML・document.write・createContextualFragment・DOMParser・eval・new Function が無い", () => {
  const dir = join(appRoot, "src", "renderer");
  const files = readdirSync(dir).filter((f) => f.endsWith(".ts") || f.endsWith(".html"));
  assert.ok(files.length >= 5);
  for (const f of files) {
    const src = readFileSync(join(dir, f), "utf8").replace(/\/\/.*$/gm, "").replace(/\/\*[\s\S]*?\*\//g, "");
    assert.doesNotMatch(src, /innerHTML|outerHTML|insertAdjacentHTML|document\.write|createContextualFragment|DOMParser|\beval\s*\(|new\s+Function/, f);
  }
});

test("[U3c-3] 道具の行の印は結果の種別ごと: 失敗の行に成功の印(✓)が付かない。data-text と行の数は変わらない", async () => {
  const m = await mount(NEW_RENDERER, NEW_HTML);
  const src: ChatLine[] = [L("tool", "ok 行", { outcome: "ok" }), L("tool", "失敗の行", { outcome: "fail" }), L("tool", "合格", { step: true })];
  m.render(chatMsg(src));
  const lis = m.doc.getElementById("chat-lines")!.children as import("./fake-dom.ts").FakeEl[];
  assert.deepEqual(lis.map((l) => l.getAttribute("data-outcome")), ["ok", "fail", "step"]);
  assert.deepEqual(m.run(scriptFor({ op: "lines" })), src.map((l) => ({ role: "tool", text: l.text })));
  const css = readFileSync(join(appRoot, "src/renderer/style.css"), "utf8");
  const marks = [...css.matchAll(/([^{}]*)\{[^}]*content:\s*"([✓✗])"/g)].map((x) => [x[1]!.trim(), x[2]!] as const);
  const okSel = marks.filter(([, c]) => c === "✓").map(([s]) => s).join(" ");
  assert.ok(okSel.includes('data-outcome="ok"') && okSel.includes('data-outcome="step"') && !okSel.includes('data-outcome="fail"'), "✓ は成功と手順の合格の行だけ");
  assert.ok(marks.every(([s]) => s.includes("data-outcome")), "種別を問わず付く印が無い");
  assert.ok(marks.some(([s, c]) => c === "✗" && s.includes('data-outcome="fail"')));
});
