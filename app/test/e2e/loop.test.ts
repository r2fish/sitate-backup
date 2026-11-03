// 試験用ドライバの規則の試験(作業指示書 5.14 の完了条件 12 の「試験」。偽のアプリ = 台本に無い問いなどを出す偽のエージェント)。
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { spawnSync } from "node:child_process";
import { A2Loop } from "./a2-loop.ts";
import { FakeApp, envelope, toolCall } from "./fake-app.ts";
import type { AgentTurn } from "./fake-app.ts";
import { Recorder } from "./record.ts";
import { B2_ENDPOINT_RE, isQuestion, matchQuestion, readJson } from "./script.ts";
import type { A2Script, Answers } from "./script.ts";

const repo = join(import.meta.dirname, "..", "..", "..");
const script = readJson<A2Script>(join(repo, "test", "acceptance", "scripts", "A2", "script.json"));
const catalog = readJson<Record<string, string>>(join(repo, "app", "src", "i18n", "ja", "catalog.json"));
const ENDINGS = ["ですか", "でしょうか", "ますか", "ませんか", "かな", "いかがですか", "よろしいですか", "お願いできますか", "いただけますか", "どうしますか", "どちらにしますか"];
const KEY = "sk-ant-fake-testsecretvalue0123456789";
const answers: Answers = { llmKey: KEY, adminKeyPem: "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAAfakefakefake\n-----END OPENSSH PRIVATE KEY-----\n", storageAdmin: "fakeid12345678\nfakesecret12345678", storageEndpoint: "http://s3.example.test", bucket: "b-test", hookUrl: "https://hook.example.test/hooks/ops-hook", adminUser: "adm", backupHost: "backup", sourceHost: "source", destHost: "dest", destPort: 22, hookRecordsFile: "/nonexistent", containers: { backup: "b", source: "s", dest: "d" } };
const L = (k: string): string => catalog[k]!;
const stepLines = (...n: number[]): { role: string; text: string }[] => n.map((i) => ({ role: "tool", text: `手順 ${i} が合格しました。` }));
const agent = (text: string): { role: string; text: string }[] => [{ role: "agent", text }];

interface Run { out: { exit: number; reason: string }; loop: A2Loop; app: FakeApp; rec: Recorder; file: string }
async function drive(turns: AgentTurn[], variant = "base"): Promise<Run> {
  const dir = mkdtempSync(join(tmpdir(), "e2e-loop-"));
  const rec = new Recorder(dir, [KEY, answers.storageAdmin]);
  // 先頭の番: 「始める」の後のエージェントの最初の応答(所在と管理アカウントを聞く)。開始時の入力の 2 つ目(chats[0])がその次の番を始める。
  const app = new FakeApp([{ lines: agent("バックアップサーバーと元サーバーの所在と管理アカウントを教えてください。") }, ...turns]);
  let t = 0;
  const loop = new A2Loop({ port: app, script, answers, rec, variant, endings: ENDINGS, catalog, hookWord: () => "word", envOp: async () => undefined, sleep: async (ms) => { t += ms; }, now: () => t, deadlineMs: 600000 });
  const out = await loop.run();
  return { out, loop, app, rec, file: rec.path };
}
const cleanup = (r: Run): void => rmSync(join(r.file, ".."), { recursive: true, force: true });
const END_FAIL: AgentTurn = { end: { outcome: "failure", code: "AGENT_EXITED" } };

test("台本に無い問い: 固定の答えが入って回が続き、問いが 1 つ増える", async () => {
  const r = await drive([{ lines: agent("ご希望の保管期間はありますか?") }, END_FAIL]);
  assert.equal(r.app.chats[1], script.unscripted_question_answer);
  assert.equal(r.loop.counts.questions, 1);
  assert.equal(r.loop.counts.unscripted, 1);
  assert.equal(r.out.exit, 1, "続いた末に次の番の失敗で止まる(固定の答えの後も回は続いた)");
  cleanup(r);
});

test("疑問符で終わらない停止: 続行の介入が 1 つ増え、問いは増えない", async () => {
  const r = await drive([{ lines: agent("次へ進みます。") }, END_FAIL]);
  assert.equal(r.app.chats[1], script.continue_input);
  assert.equal(r.loop.counts.continues, 1);
  assert.equal(r.loop.counts.questions, 0);
  cleanup(r);
});

test("問いの終わり方の一覧(疑問符が無い文末)でも問いと数える", async () => {
  assert.equal(isQuestion("この案でよろしいでしょうか", ENDINGS), true);
  assert.equal(isQuestion("次へ進みます。", ENDINGS), false);
  assert.equal(isQuestion("確認します。何を守りたいですか?", ENDINGS), true);
  assert.equal(matchQuestion("保管先のエンドポイントとバケットと通知先を教えてください", script), "Q1");
  assert.equal(matchQuestion("今日の天気は", script), null);
});

test("Q4 の示し直しで問いの回数が増えない(希望 → 案を受ける → 直した案も受ける)", async () => {
  const r = await drive([
    { lines: agent("何を守りたいですか?") },
    { lines: agent("この案でよろしいでしょうか?") },
    { lines: agent("案を直しました。この案でよろしいでしょうか?") },
    END_FAIL,
  ], "lo");
  assert.equal(r.app.chats[1], script.questions.Q4.answers!.large_object, "変種 lo の希望");
  assert.equal(r.app.chats[2], script.questions.Q4.accept_proposal);
  assert.equal(r.app.chats[3], script.questions.Q4.accept_proposal);
  assert.equal(r.loop.counts.questions, 1);
  cleanup(r);
});

test("AGENT_TURN_LIMIT の後は台本の続けるための入力を入れて続け、別に数える", async () => {
  const r = await drive([{ end: { outcome: "failure", code: "AGENT_TURN_LIMIT" } }, END_FAIL]);
  assert.equal(r.app.chats[1], script.turn_limit_input);
  assert.equal(r.loop.counts.turnLimit, 1);
  assert.equal(r.loop.counts.continues, 0);
  cleanup(r);
});

test("[mp-a2] 隠した応答の後は古い問いに答え直さず、続行の介入になる。記録に本文を残さない", async () => {
  const r = await drive([{ lines: agent("ご希望の保管期間はありますか?") }, { lines: [{ role: "notice", text: L("chat.quote.rejected_guide") }] }, END_FAIL]);
  assert.equal(r.app.chats[1], script.unscripted_question_answer);
  assert.equal(r.app.chats[2], script.continue_input, "隠した応答の後は続行の入力");
  assert.equal(r.loop.counts.withheld, 1);
  const rec = readFileSync(r.file, "utf8");
  assert.match(rec, /"kind":"withheld_reply"[^\n]*"count":1/);
  assert.ok(!/"kind":"withheld_reply"[^\n]*"text"/.test(rec), "本文を残さない");
  cleanup(r);
});

test("[mp-a2] LLM_RATE_LIMITED: 待って続行の入力で再試行し、記録に残す。上限を超えたら失敗で終える", async () => {
  const r = await drive([{ end: { outcome: "failure", code: "LLM_RATE_LIMITED" } }, { lines: agent("次へ進みます。") }, END_FAIL]);
  assert.equal(r.app.chats[1], script.continue_input);
  assert.equal(r.loop.counts.rateRetries, 1);
  assert.match(readFileSync(r.file, "utf8"), /"kind":"rate_limit_wait"/);
  cleanup(r);
  const lim = (): AgentTurn => ({ end: { outcome: "failure", code: "LLM_RATE_LIMITED" } });
  const r2 = await drive(Array.from({ length: 8 }, lim));
  assert.equal(r2.out.exit, 1);
  assert.match(r2.out.reason, /LLM_RATE_LIMITED/);
  assert.equal(r2.loop.counts.rateRetries, 5);
  cleanup(r2);
});

test("台本に無いカード(seal 以外)で終了コード 2", async () => {
  const r = await drive([{ surface: { card: { proofs: [], actions: [{ action: "approve", disabled: false }], text: L("card.op.delay-change.title") } } }]);
  assert.equal(r.out.exit, 2);
  cleanup(r);
});

test("台本に無い秘密の入力ボックス(一般の文)で終了コード 2", async () => {
  const r = await drive([{ surface: { secret: L("chat.secret.label.generic") } }]);
  assert.equal(r.out.exit, 2);
  cleanup(r);
});

test("`source survey` の /result/prereq_missing が空でなければ終了コード 3(H5。無効)", async () => {
  const r = await drive([{ events: [toolCall("sitate_source_survey", { result: { prereq_missing: ["rsync"] } })], lines: agent("調べました。") }]);
  assert.equal(r.out.exit, 3);
  cleanup(r);
  const ok = await drive([{ events: [toolCall("sitate_source_survey", { result: { prereq_missing: [] } })], lines: agent("次へ進みます。") }, END_FAIL]);
  assert.equal(ok.out.exit, 1, "空なら H5 ではない");
  cleanup(ok);
});

test("完了: 手順 1〜13 の合格と status の 3 値が揃えば終了コード 0。人の作業 H1 を数える", async () => {
  const status = toolCall("sitate_status", { result: { last_backup: { ok: true }, last_restore_test: { ok: true }, notify: { proof_confirmed: true } } }, true);
  const r = await drive([
    { surface: { secret: L("chat.secret.label.storage_admin") }, events: [status], lines: [...stepLines(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13), ...agent("導入が完了しました。")] },
  ]);
  assert.equal(r.out.exit, 0, r.out.reason);
  assert.deepEqual([...r.loop.humanWork], ["H1"]);
  assert.equal(r.loop.stepsPassed.size, 13);
  cleanup(r);
  const bad = toolCall("sitate_status", { result: { last_backup: { ok: true }, last_restore_test: { ok: false }, notify: { proof_confirmed: true } } });
  const r2 = await drive([{ events: [bad], lines: [...stepLines(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13), ...agent("完了")] }]);
  assert.equal(r2.out.exit, 1, "status の値が揃わなければ不合格");
  cleanup(r2);
});

test("自力で立て直せた割合: ok:false の後に次の手順の合格が来れば立て直せた", async () => {
  const failed = { kind: "tool_call", name: "sitate_doctor", envelope: envelope("doctor", { ok: false, error: { code: "DOCTOR_FAILED" } }), step_passed: false };
  const r = await drive([{ events: [failed], lines: [...stepLines(1), ...agent("次へ進みます。")] }, END_FAIL]);
  assert.deepEqual(r.loop.recovery(), { failures: 1, recovered: 1 });
  cleanup(r);
});

test("記録は秘密の値を出さない(入力ボックスに入れた値は種類だけ。書く直前の検査でも伏せる)", async () => {
  const r = await drive([{ surface: { secret: L("chat.secret.label.llm") }, lines: agent("次へ進みます。") }, END_FAIL]);
  assert.equal(r.rec.redactions, 0, "ドライバの通常の記録は値を書こうとしない");
  assert.ok(!readFileSync(r.file, "utf8").includes(KEY));
  assert.ok(readFileSync(r.file, "utf8").includes('"label_key":"chat.secret.label.llm"'));
  r.rec.write("probe", { text: `キーは ${KEY} です` });
  const text = readFileSync(r.file, "utf8");
  assert.ok(!text.includes(KEY) && !text.includes(Buffer.from(KEY).toString("base64")));
  assert.equal(r.rec.redactions, 1, "書く直前の検査が値を伏せた");
  cleanup(r);
});

test("コマンドラインの入口: 引数が足りなければ終了コード 64", () => {
  const r = spawnSync(process.execPath, [join(import.meta.dirname, "cli.ts")], { encoding: "utf8" });
  assert.equal(r.status, 64);
  assert.match(r.stderr, /--script と --out/);
});

test("[mp-a2] B2 の端点の形: https://s3.<リージョン>.backblazeb2.com だけ(https:// 無し・末尾の / ・経路は不可)", () => {
  assert.ok(B2_ENDPOINT_RE.test("https://s3.us-west-004.backblazeb2.com"));
  for (const bad of ["s3.us-west-004.backblazeb2.com", "https://s3.us-west-004.backblazeb2.com/", "http://s3.us-west-004.backblazeb2.com", "https://s3.us-west-004.backblazeb2.com/bucket"]) assert.ok(!B2_ENDPOINT_RE.test(bad), bad);
});

test("[mp-a2 D-0169] 開始: 固定の文を打たず「始める」を押し、最初の応答の後に所在と管理アカウントを渡す。どちらも問いに数えない", async () => {
  const r = await drive([END_FAIL]);
  assert.equal(r.app.starts, 1);
  assert.deepEqual(r.app.clicks[0], "chat:chat-start");
  assert.match(r.app.chats[0]!, /バックアップサーバー: backup.*管理アカウント: adm/);
  assert.ok(!r.app.chats[0]!.includes(script.continue_input));
  assert.equal(r.loop.counts.questions, 0);
  assert.equal(r.loop.counts.continues, 0);
  const rec = readFileSync(r.file, "utf8");
  assert.match(rec, /"kind":"start_click"/);
  cleanup(r);
});
