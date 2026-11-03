// 問いの出し方の規則(spec 3.7、D-0171)の試験: ドライバの判定と記録、記録の退避、偽の LLM の口の発話。
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { A2Loop } from "./a2-loop.ts";
import { planOf } from "./fake-agent.ts";
import { FakeApp } from "./fake-app.ts";
import type { AgentTurn } from "./fake-app.ts";
import { archiveExisting, Recorder } from "./record.ts";
import { isQuestion, questionForm, readJson } from "./script.ts";
import type { A2Script, Answers } from "./script.ts";

const repo = join(import.meta.dirname, "..", "..", "..");
const script = readJson<A2Script>(join(repo, "test", "acceptance", "scripts", "A2", "script.json"));
const catalog = readJson<Record<string, string>>(join(repo, "app", "src", "i18n", "ja", "catalog.json"));
const ENDINGS = ["ですか", "でしょうか", "ますか", "ませんか", "いただけますか"];
const answers: Answers = { llmKey: "sk-ant-fake-testsecretvalue0123456789", adminKeyPem: "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAAfakefakefake\n-----END OPENSSH PRIVATE KEY-----\n", storageAdmin: "fakeid12345678\nfakesecret12345678", storageEndpoint: "http://s3.example.test", bucket: "b-test", hookUrl: "https://hook.example.test/hooks/ops-hook", adminUser: "adm", backupHost: "backup", sourceHost: "source", destHost: "dest", destPort: 22, hookRecordsFile: "/nonexistent", containers: { backup: "b", source: "s", dest: "d" } };
const agent = (text: string): { role: string; text: string }[] => [{ role: "agent", text }];
const END_FAIL: AgentTurn = { end: { outcome: "failure", code: "AGENT_EXITED" } };

test("[D-0171] 問いの出し方: 問いが最後の文でない・問いが 2 つ以上を見分ける", () => {
  assert.deepEqual(questionForm("何を守りたいですか? 推奨の案は全部です。", ENDINGS), { notLast: true, multi: false });
  assert.deepEqual(questionForm("Q3 を聞きます。復元先はどこですか? 次に何を守りたいですか?", ENDINGS), { notLast: false, multi: true });
  assert.deepEqual(questionForm("推奨の案を添えます。復元先の所在を教えていただけますか?", ENDINGS), { notLast: false, multi: false });
  assert.deepEqual(questionForm("次へ進みます。", ENDINGS), { notLast: false, multi: false }, "問いを含まない発話は違反に数えない");
  assert.equal(isQuestion("復元先の所在を教えてください。", ENDINGS), false, "「教えてください」は問いの終わり方でない");
});

test("[D-0171] ドライバは違反を a2_fields の元の数に数え、同じ依頼の繰り返しの続行の介入は 3 回目で印を付ける", async () => {
  const dir = mkdtempSync(join(tmpdir(), "e2e-form-"));
  const rec = new Recorder(dir, [answers.llmKey]);
  const turns: AgentTurn[] = [{ lines: agent("復元先はどこですか? 説明を続けます。") }, { lines: agent("Q3 は復元先はどこですか? Q4 は何を守りたいですか?") },
    ...[1, 2, 3].map(() => ({ lines: agent("復元先の所在を教えてください。") })), END_FAIL];
  const app = new FakeApp([{ lines: agent("所在を教えていただけますか?") }, ...turns]);
  let t = 0;
  const loop = new A2Loop({ port: app, script, answers, rec, variant: "base", endings: ENDINGS, catalog, hookWord: () => "w", envOp: async () => undefined, sleep: async (ms) => { t += ms; }, now: () => t, deadlineMs: 600000 });
  await loop.run();
  assert.equal(loop.counts.questionNotLast, 1);
  assert.equal(loop.counts.multiQuestion, 1);
  assert.equal(loop.counts.continues, 4, "疑問符で終わらない 3 回と、問いの後に文が続く 1 回");
  assert.equal(loop.counts.repeatedRequests, 1, "同じ文の繰り返しが 3 回で印は 1 つ");
  const text = readFileSync(rec.path, "utf8");
  assert.match(text, /"kind":"repeated_request"/);
  assert.match(text, /"kind":"question_not_last"/);
  assert.match(text, /"kind":"multi_question"/);
  rmSync(dir, { recursive: true, force: true });
});

test("[D-0171] --out の既存の record.jsonl は日時つきの名前に退避してから書く", () => {
  const dir = mkdtempSync(join(tmpdir(), "e2e-archive-"));
  assert.equal(archiveExisting(dir), null, "無ければ何もしない");
  writeFileSync(join(dir, "record.jsonl"), "{\"kind\":\"old\"}\n");
  const to = archiveExisting(dir, new Date(2026, 10, 3, 9, 8, 7));
  assert.ok(to !== null && to.endsWith("record.20261103T090807.jsonl"));
  assert.ok(!existsSync(join(dir, "record.jsonl")));
  new Recorder(dir, []).write("new");
  assert.equal(readFileSync(to, "utf8"), "{\"kind\":\"old\"}\n", "前の回の記録は混ざらない");
  assert.equal(readFileSync(join(dir, "record.jsonl"), "utf8").match(/old/), null);
  writeFileSync(join(dir, "record.jsonl"), "x\n");
  const again = archiveExisting(dir, new Date(2026, 10, 3, 9, 8, 7));
  assert.notEqual(again, to, "同じ秒でも上書きしない");
  assert.equal(readdirSync(dir).filter((f) => f.startsWith("record.2")).length, 2);
  rmSync(dir, { recursive: true, force: true });
});

test("[D-0171] 偽の LLM の口の発話は、問いを 1 つだけ、最後の文にしている", async () => {
  const plan = planOf(answers, async () => undefined);
  let spoken = 0;
  for (const turn of plan) for (const step of turn) {
    const r = step.make("");
    if (r.text === undefined) continue;
    spoken += 1;
    const f = questionForm(r.text, ENDINGS);
    assert.deepEqual(f, { notLast: false, multi: false }, r.text);
  }
  assert.ok(spoken >= 6);
});
