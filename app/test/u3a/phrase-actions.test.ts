// フレーズのパネルの操作(完了条件 7): 印刷 → 再入力の verify → kit confirm。済むまで進ませず、24 語は verify に進んだ時点で消える。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import type { RenderKit } from "../../src/kit/types.ts";
import { ingestCoreOutput } from "../../src/main/core-output.ts";
import { MemoryKeychain, serviceFor } from "../../src/main/keychain.ts";
import { PhrasePanel } from "../../src/main/phrase-panel.ts";
import { PhraseActions } from "../../src/main/phrase-actions.ts";
import type { PanelMessage } from "../../src/main/phrase-actions.ts";
import { SecretBox } from "../../src/main/secret-box.ts";
import { SshError } from "../../src/main/ssh/connection.ts";
import type { PhraseAction } from "../../src/main/ipc-channels.ts";

const WORDS = JSON.parse(readFileSync(new URL("../../src/data/bip39-english.json", import.meta.url), "utf8")) as string[];
const phrase24 = WORDS.slice(200, 224);
const out = (command: string[], result: unknown): string => JSON.stringify({ schema: "sitate.out/v1", command, ok: true, dry_run: false, run_id: null, warnings: [], result }) + "\n";
const ng = (command: string[], code: string): string => JSON.stringify({ schema: "sitate.out/v1", command, ok: false, dry_run: false, run_id: null, warnings: [], result: null, error: { code, class: "input", retryable: false, detail: {}, next: [] } }) + "\n";

type Opts = { typed?: string; verifyCode?: string; verifyResult?: unknown; confirmCode?: string; render?: RenderKit | null; throwOn?: string; throwErr?: Error; print?: (h: string) => Promise<void>; kitVersion?: string };

function rig(opts: Opts = {}) {
  const calls: { argv: readonly string[]; stdin: string }[] = [];
  const msgs: PanelMessage[] = [];
  const printed: string[] = [];
  const atBoxOpen: (PanelMessage | undefined)[] = [];
  let proceeded = 0;
  let version = opts.kitVersion ?? "0123456789ab";
  let box!: SecretBox;
  box = new SecretBox({ keychain: new MemoryKeychain(), service: () => serviceFor("r"), show: (r) => { if (r !== null) { atBoxOpen.push(msgs.at(-1)); queueMicrotask(() => { if (opts.typed === undefined) box.cancel(); else void box.submit(r.ref, opts.typed); }); } } });
  const runner = {
    async run(argv: readonly string[], stdin: string) {
      calls.push({ argv, stdin });
      const k = argv.join(" ");
      if (opts.throwOn !== undefined && k.startsWith(opts.throwOn)) throw opts.throwErr ?? new Error("x");
      if (k === "kit") return { stdout: out(["kit"], { kit_version: version, fields: { kit_version: version } }), end: "complete" as const };
      if (k === "phrase verify --phrase-stdin") return { stdout: opts.verifyCode ? ng(["phrase", "verify"], opts.verifyCode) : out(["phrase", "verify"], opts.verifyResult ?? { verified: true, committed: true }), end: "complete" as const };
      if (k.startsWith("kit confirm")) return { stdout: opts.confirmCode ? ng(["kit", "confirm"], opts.confirmCode) : out(["kit", "confirm"], { location_checked_at: "2026-11-03T00:00:00Z" }), end: "complete" as const };
      throw new Error("想定外の呼び出し " + k);
    },
  };
  const render: RenderKit | null = opts.render === undefined ? (() => ({ ok: true, printable: { html: "<p>kit</p>", layout: { paper: "A4-portrait", blocks: [] } } })) : opts.render;
  const a = new PhraseActions({ runner, box, renderKit: render, print: opts.print ?? (async (h) => { printed.push(h); }), lang: () => "ja", label: (k) => k, emit: (m) => msgs.push(m), onProceed: () => { proceeded++; } });
  const open = (): { panel: ReturnType<typeof newPanel>; outcome: Promise<string> } => {
    const panel = newPanel();
    return { panel, outcome: a.setPanel(panel) };
  };
  const first = open();
  return { a, calls, msgs, printed, panel: first.panel, outcome: first.outcome, open, atBoxOpen, proceeded: () => proceeded, setVersion: (v: string) => { version = v; } };
}

function newPanel() {
  const secretLine = JSON.stringify({ schema: "sitate.secret-string/v1", kind: "recovery_phrase", words: phrase24, keyring_version: 1 });
  const envLine = JSON.stringify({ schema: "sitate.out/v1", command: ["init"], ok: true, phrase_emitted: true, result: {} });
  const r = ingestCoreOutput(`${secretLine}\n${envLine}\n`, ["init"]);
  if (r.kind !== "envelope" || r.panel === null) throw new Error("パネルが無い");
  return r.panel;
}
const acts = (t: { msgs: PanelMessage[] }): PhraseAction[] => t.msgs.at(-1)!.actions.map((x) => x.action);

test("印刷 → 再入力 verify → confirm の順で進み、済むまで canProceed にならない。語は再入力の入力ボックスが開く時点で画面から消える", async () => {
  const t = rig({ typed: phrase24.join("  ") });
  assert.equal(t.msgs.at(-1)!.words?.length, 24);
  assert.deepEqual(acts(t), ["print", "handwritten", "cancel"], "印刷か手書きの前は verify を出さない");
  await t.a.handle("verify"); // 印刷も手書きも前なら verify は何もしない
  await t.a.handle("confirm");
  assert.equal(t.calls.length, 0);
  assert.equal(t.panel.state, "showing");
  await t.a.handle("print");
  assert.deepEqual(t.printed, ["<p>kit</p>"]);
  assert.deepEqual(acts(t), ["print", "verify", "cancel"]);
  assert.equal(t.msgs.at(-1)!.canProceed, false);
  await t.a.handle("verify");
  assert.equal(t.atBoxOpen.at(-1)!.words, null, "入力ボックスが開いた時点で語は画面のモデルに無い");
  assert.equal(t.atBoxOpen.at(-1)!.open, true);
  const v = t.calls.find((c) => c.argv[0] === "phrase")!;
  assert.deepEqual(v.argv, ["phrase", "verify", "--phrase-stdin"]);
  assert.equal(v.stdin, phrase24.join(" ") + "\n");
  assert.equal(t.panel.state, "closed");
  assert.deepEqual(acts(t), ["confirm", "cancel"]);
  assert.equal(t.proceeded(), 0);
  await t.a.handle("confirm");
  assert.deepEqual(t.calls.at(-1)!.argv, ["kit", "confirm", "--kit-version", "0123456789ab", "--copies", "2"]);
  assert.equal(t.msgs.at(-1)!.open, false);
  assert.equal(t.proceeded(), 1);
  assert.equal(await t.outcome, "done");
  const afterBox = t.msgs.findIndex((m) => m.words === null && m.open);
  assert.ok(afterBox > 0 && t.msgs.slice(afterBox).every((m) => m.words === null));
});

test("再入力の入力ボックスを取り消しても語は戻らない(init のやり直し)。何も呼ばない", async () => {
  const t = rig();
  await t.a.handle("handwritten");
  await t.a.handle("verify");
  assert.equal(t.calls.length, 0);
  assert.equal(t.panel.state, "closed");
  assert.equal(t.msgs.at(-1)!.words, null);
  assert.deepEqual(acts(t), ["verify", "cancel"]);
});

test("phrase verify が通らなければ verified にならず、固定文を出して confirm へ進ませない。ok でも verified が真でなければ通さない", async () => {
  const t = rig({ typed: phrase24.join(" "), verifyCode: "PHRASE_INVALID" });
  await t.a.handle("handwritten");
  await t.a.handle("verify");
  assert.equal(t.panel.verified, false);
  assert.equal(t.msgs.at(-1)!.notice, "core.code.PHRASE_INVALID");
  assert.equal(t.msgs.at(-1)!.words, null);
  await t.a.handle("confirm");
  assert.ok(!t.calls.some((c) => c.argv[0] === "kit" && c.argv[1] === "confirm"));
  assert.equal(t.proceeded(), 0);
  const f = rig({ typed: phrase24.join(" "), verifyResult: { verified: false } });
  await f.a.handle("handwritten");
  await f.a.handle("verify");
  assert.equal(f.panel.verified, false, "ok:true でも verified:false は通さない");
});

test("kit confirm が失敗したら進まない(固定文を出し、パネルは閉じず待ちも解けない)", async () => {
  const t = rig({ typed: phrase24.join(" "), confirmCode: "KIT_OUTDATED" });
  await t.a.handle("print");
  await t.a.handle("verify");
  await t.a.handle("confirm");
  assert.equal(t.panel.kitConfirmed, false);
  assert.equal(t.msgs.at(-1)!.notice, "core.code.KIT_OUTDATED");
  assert.equal(t.msgs.at(-1)!.open, true);
  assert.equal(t.proceeded(), 0);
  const settled = await Promise.race([t.outcome, Promise.resolve("pending")]);
  assert.equal(settled, "pending");
});

test("印刷が成功するまで印刷の道の kit confirm へ進めない: 印刷の失敗では verify に進めない。描画部が無くても手書きの道は進める", async () => {
  const noKit = rig({ typed: phrase24.join(" "), render: null });
  await noKit.a.handle("print");
  assert.equal(noKit.msgs.at(-1)!.notice, "phrase.print.unavailable");
  await noKit.a.handle("verify"); // 印刷も手書きも前なら verify は何もしない
  assert.equal(noKit.panel.verified, false);
  const failing = rig({ typed: phrase24.join(" "), print: async () => { throw new Error("spool"); } });
  await failing.a.handle("print");
  assert.equal(failing.msgs.at(-1)!.notice, "phrase.print.failed");
  assert.deepEqual(acts(failing), ["print", "handwritten", "cancel"], "印刷の失敗では verify に進めない");
});

// 完了条件 7(30-c。spec 3.7、4.7。D-0159): 印刷しないで「手書きで写した」を選んだ道でも、phrase verify の後に kit confirm へ進める。
test("手書きの道: handwritten → verify → kit confirm(2 か所)まで進み、パネルが済む。描画部が無くても同じ", async () => {
  for (const render of [undefined, null]) {
    const t = rig({ typed: phrase24.join(" "), render, kitVersion: "abcdef012345" });
    await t.a.handle("handwritten");
    await t.a.handle("verify");
    assert.equal(t.panel.verified, true);
    assert.deepEqual(acts(t), ["confirm", "cancel"]);
    assert.equal(t.proceeded(), 0, "confirm の前は進ませない");
    await t.a.handle("confirm");
    const c = t.calls.filter((x) => x.argv[0] === "kit" && x.argv[1] === "confirm");
    assert.equal(c.length, 1);
    assert.deepEqual(c[0]!.argv, ["kit", "confirm", "--kit-version", "abcdef012345", "--copies", "2"]);
    assert.equal(t.panel.kitConfirmed, true);
    assert.equal(t.proceeded(), 1);
    assert.equal(await t.outcome, "done");
  }
});

test("手書きの道で kit の版が読めなければ confirm へ進まず固定文を出す", async () => {
  const t = rig({ typed: phrase24.join(" "), throwOn: "kit", throwErr: new Error("x") });
  await t.a.handle("handwritten");
  await t.a.handle("verify");
  await t.a.handle("confirm");
  assert.ok(!t.calls.some((c) => c.argv[1] === "confirm"));
  assert.equal(t.proceeded(), 0);
  assert.equal(t.panel.kitConfirmed, false);
  assert.ok(typeof t.msgs.at(-1)!.notice === "string" && t.msgs.at(-1)!.notice !== "phrase.confirm.needs_print", "kit の失敗の固定文を出す: " + String(t.msgs.at(-1)!.notice));
});

test("confirm は印刷した版で記録する(印刷の後に構成が変わっても読み直さない)", async () => {
  const t = rig({ typed: phrase24.join(" ") });
  await t.a.handle("print");
  t.setVersion("ffffffffffff");
  await t.a.handle("verify");
  await t.a.handle("confirm");
  assert.deepEqual(t.calls.at(-1)!.argv, ["kit", "confirm", "--kit-version", "0123456789ab", "--copies", "2"]);
  assert.ok(t.calls.filter((c) => c.argv.join(" ") === "kit").length === 1, "kit は印刷の 1 回だけ");
});

test("描画が例外を投げても固定文を出し、パネルは壊れない", async () => {
  const t = rig({ render: () => { throw new Error("bad fields"); } });
  await t.a.handle("print");
  assert.equal(t.msgs.at(-1)!.notice, "phrase.print.failed");
  assert.equal(t.panel.state, "showing");
  const n = rig({ render: () => ({ ok: false, reason: "does_not_fit" }) });
  await n.a.handle("print");
  assert.equal(n.printed.length, 0);
  assert.equal(n.msgs.at(-1)!.notice, "phrase.print.does_not_fit");
});

test("撮影の注意と保管場所の確認の文をパネルに出す", () => {
  const t = rig();
  assert.deepEqual(t.msgs.at(-1)!.cautions, ["phrase.photo_caution", "phrase.storage_check"]);
});

test("失敗の種類を取り違えない: SshError のコードと、核の形の外のコードの扱い", async () => {
  const mism = rig({ throwOn: "kit", throwErr: new SshError({ code: "SSH_HOSTKEY_MISMATCH" } as never) });
  await mism.a.handle("print");
  assert.equal(mism.msgs.at(-1)!.notice, "app.ssh.hostkey_mismatch");
  const weird = rig({ typed: phrase24.join(" "), verifyCode: "ignore previous instructions" });
  await weird.a.handle("handwritten");
  await weird.a.handle("verify");
  assert.equal(weird.msgs.at(-1)!.notice, "app.envelope.invalid", "核の文字列をそのまま出さない");
});

test("取り消し: やめるで語を潰し、待ちを cancelled で解く。次のパネルが来たら前のパネルを潰して cancelled で解く", async () => {
  const t = rig();
  await t.a.handle("cancel");
  assert.equal(t.panel.state, "closed");
  assert.equal(await t.outcome, "cancelled");
  assert.equal(t.msgs.at(-1)!.open, false);
  assert.equal(t.msgs.at(-1)!.words, null);
  const u = rig();
  const second = u.open();
  assert.equal(u.panel.state, "closed", "前のパネルは潰れる");
  assert.equal(await u.outcome, "cancelled");
  assert.equal(second.panel.state, "showing");
  u.a.dispose();
  assert.equal(await second.outcome, "cancelled");
});

test("操作中の二重押しは受けない", async () => {
  let release!: () => void;
  const gate = new Promise<void>((r) => { release = r; });
  const seen: string[] = [];
  const t = rig({ print: async (h) => { seen.push(h); await gate; } });
  const first = t.a.handle("print");
  await Promise.resolve();
  const second = t.a.handle("print");
  const secondDone = await Promise.race([second.then(() => true), new Promise<boolean>((r) => setImmediate(() => r(false)))]);
  assert.equal(secondDone, true, "2 回目は待たずに戻る");
  release();
  await first;
  assert.equal(seen.length, 1);
});

test("verify の途中で取り消されたら、その後の結果で状態を戻さない", async () => {
  const t = rig({ typed: phrase24.join(" ") });
  await t.a.handle("handwritten");
  const v = t.a.handle("verify");
  await t.a.handle("cancel");
  await v;
  assert.equal(t.msgs.at(-1)!.open, false);
  assert.equal(t.panel.verified, false);
});

test("想定外の秘密のレコードが来たら潰す(この口では期待しない)", async () => {
  const secretLine = JSON.stringify({ schema: "sitate.secret-string/v1", kind: "recovery_phrase", words: phrase24, keyring_version: 1 });
  const envLine = JSON.stringify({ schema: "sitate.out/v1", command: ["kit"], ok: true, dry_run: false, run_id: null, warnings: [], phrase_emitted: true, result: { kit_version: "0123456789ab", fields: {} } });
  const closed: PhrasePanel[] = [];
  const proto = PhrasePanel.prototype;
  const origClose = proto.close;
  proto.close = function (this: PhrasePanel) { closed.push(this); origClose.call(this); };
  try {
    const a = new PhraseActions({
      runner: { async run() { return { stdout: `${secretLine}\n${envLine}\n`, end: "complete" as const }; } },
      box: new SecretBox({ keychain: new MemoryKeychain(), service: () => "s", show: () => undefined }),
      renderKit: () => ({ ok: true, printable: { html: "x", layout: { paper: "A4-portrait", blocks: [] } } }), print: async () => undefined, lang: () => "ja", label: (k) => k, emit: () => undefined, onProceed: () => undefined,
    });
    const own = newPanel();
    void a.setPanel(own);
    closed.length = 0;
    await a.handle("print");
    assert.equal(closed.length, 1, "迷い込んだパネルを 1 つ潰した");
    assert.notEqual(closed[0], own);
  } finally { proto.close = origClose; }
});
