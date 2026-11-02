// Electron の主プロセスの入口。起動時に同梱物の署名を検証し(失敗なら固定文だけを出して終わる)、キーチェーンから LLM API キーを取り出して
// utilityProcess へ IPC で渡し、画面(レンダラ)とエージェントの間を仲介する。画面とエージェントのプロセスは直接通信しない。
// 管理用 SSH(CoreRunner の実体と bootstrap)は主プロセスだけが持つ(ssh/、bootstrap/)。
import { app, dialog, ipcMain, Notification, utilityProcess } from "electron";
import type { BrowserWindow } from "electron";
import { renderKit } from "../kit/render.ts";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { AgentHost } from "./agent-host.ts";
import { AppRecorder } from "./app-record.ts";
import { SoftwareApprovalKey } from "./approval-sign.ts";
import { loadBundle } from "./bundle-load.ts";
import type { LoadedBundle } from "./bundle-load.ts";
import type { TrustedKey } from "./minisign.ts";
import { BUNDLE_REFUSAL } from "./bundle-verify.ts";
import { runBootstrap } from "./bootstrap/bootstrap.ts";
import type { BootstrapInput, BundleReader } from "./bootstrap/bootstrap.ts";
import { reportFailure } from "./failure-report.ts";
import { CHANNEL } from "./ipc-channels.ts";
import type { CardDecision, UiMessage } from "./ipc-channels.ts";
import { MacKeychain, SERVICE_PRE_ADMIN, SERVICE_PRE_LLM, serviceFor } from "./keychain.ts";
import { PathRefTable } from "./path-ref.ts";
import { LLM_ACCOUNT, SECRET_LABEL, SecretBox } from "./secret-box.ts";
import { Session } from "./session.ts";
import type { ChatLine } from "./session.ts";
import { SshCoreRunner } from "./ssh/core-runner.ts";
import { TargetStore } from "./ssh/target-store.ts";
import { StatusView } from "./status-view.ts";
import { StepJudge } from "./step-judge.ts";
import { ToolRegistry } from "./tool-registry.ts";
import { createMainWindow, printHtml } from "./windows.ts";
import { PhraseActions } from "./phrase-actions.ts";
import { buildSurveyView } from "./survey-view.ts";
import type { SurveyView } from "./survey-view.ts";
import { TargetActions } from "./target-actions.ts";
import { translator } from "../i18n/format.ts";
import { trustedKeysOf } from "../trust/embedded.ts";
import type { EmbeddedKeys } from "../trust/embedded.ts";

const APPROVAL_ACCOUNT = "approval-key";
const ADMIN_KEY_ACCOUNT = "admin-ssh-key";
/** 管理用 SSH の接続の時限(測定項目。初期値)。 */
const CONNECT_TIMEOUT_MS = 20000;

declare const __TEST_BUILD__: boolean;

interface Ctx {
  readonly res: string;
  readonly locale: string;
  readonly keychain: MacKeychain;
  readonly box: SecretBox;
  readonly host: AgentHost;
  readonly session: Session;
  readonly phrase: PhraseActions;
  readonly targetUi: TargetActions;
  readonly runner: SshCoreRunner;
  readonly label: (key: string) => string;
  readonly registry: ToolRegistry;
  readonly bundle: Extract<LoadedBundle, { ok: true }>;
  readonly targets: TargetStore;
  readonly cardResolver: { current: ((d: CardDecision) => void) | null };
  readonly typedFp: { current: string | null };
  readonly renderStatus: () => void;
}

async function main(): Promise<void> {
  await app.whenReady();
  const res = join(app.getAppPath(), "dist");
  const embedded = JSON.parse(readFileSync(join(res, "embedded-keys.json"), "utf8")) as EmbeddedKeys;
  const keys = trustedKeysOf(embedded);
  const locale = app.getLocale().startsWith("ja") ? "ja" : "en";
  const bundle = loadBundle(join(res, "bundle"), app.getVersion(), keys, locale);
  if (!bundle.ok) {
    dialog.showErrorBox("Sitate-backup", `${BUNDLE_REFUSAL.ja}\n${BUNDLE_REFUSAL.en}`);
    app.quit();
    return;
  }
  const win = createMainWindow(res);
  const ctx = await compose(res, locale, keys, bundle, win);
  registerIpc(ctx);
  app.on("before-quit", () => { ctx.phrase.dispose(); void ctx.session.dispose(); });
  // 試験用のアプリだけ(製品のビルドは __TEST_BUILD__ = false で、この枝と ../testing は束ねられない)。
  if (__TEST_BUILD__) (await import("../testing/attach.ts")).attachDriver((script) => win.webContents.executeJavaScript(script));
  ctx.renderStatus();
  await startAgent(ctx, false);
  if (ctx.targets.read() !== null) await ctx.session.refreshStatus();
}

async function approvalKeyOf(keychain: MacKeychain): Promise<SoftwareApprovalKey> {
  const stored = await keychain.get(serviceFor("app"), APPROVAL_ACCOUNT);
  const key = stored === null ? SoftwareApprovalKey.generate() : SoftwareApprovalKey.fromPkcs8(Buffer.from(stored, "base64"));
  if (stored === null) await keychain.set(serviceFor("app"), APPROVAL_ACCOUNT, key.exportPkcs8().toString("base64"));
  return key;
}

async function compose(res: string, locale: string, keys: TrustedKey[], b: Extract<LoadedBundle, { ok: true }>, win: BrowserWindow): Promise<Ctx> {
  const t = translator(JSON.parse(b.byPath(`i18n/${locale}/catalog.json`)) as Record<string, string>, locale);
  const registry = new ToolRegistry(b.paths("tools/").filter((p) => p.endsWith(".json")).map((p) => JSON.parse(b.byPath(p)) as ConstructorParameters<typeof ToolRegistry>[0][number]));
  const keychain = new MacKeychain();
  const ui = (m: UiMessage): void => win.webContents.send(CHANNEL.render, m);
  const chat: ChatLine[] = [];
  const notices: string[] = [];
  const dataDir = app.getPath("userData");
  const targets = new TargetStore(join(dataDir, "ssh-target.json"));
  const recorder = new AppRecorder(join(dataDir, "app-record.json"));
  const cardResolver: Ctx["cardResolver"] = { current: null };
  const typedFp: Ctx["typedFp"] = { current: null };
  const osNotify = (text: string): void => { if (Notification.isSupported()) new Notification({ title: "Sitate-backup", body: text }).show(); };
  const survey: { current: SurveyView | null } = { current: null };
  const targetUi = new TargetActions({ targets, typedFp, label: t, onChange: () => renderStatus() });
  const status = new StatusView({ t, now: () => new Date(), agentRunning: () => host.running, notify: osNotify });
  status.setLocale(locale);
  const renderStatus = (): void => {
    const v = status.view();
    ui({ surface: "status", heading: t("status.panel.heading"), rows: [...notices.map((n) => ({ label: n, value: "" })), ...v.rows, ...v.appInfo], askAi: v.askAi, target: targetUi.view(), survey: survey.current });
  };
  const renderChat = (): void => ui({ surface: "chat", lines: chat, sendLabel: t("chat.send.label") });
  const box = new SecretBox({ keychain, service: () => serviceFor("repo"), show: (r) => { if (r !== null) ui({ surface: "secret-box", mode: r.mode, ref: r.ref, label: t(r.label), submitLabel: t("chat.secret.submit.label") }); } });
  const failure = (code: string): void => reportFailure(code, {
    t, showNotice: (x) => { notices.push(x); renderStatus(); }, chat: (x) => { chat.push({ role: "notice", text: x }); renderChat(); }, osNotify,
    replaceKey: () => void startAgent(ctxRef.current!, true),
  });
  const host = new AgentHost((entry, o) => utilityProcess.fork(entry, [...o.args], { env: { ...o.env }, serviceName: o.serviceName, execArgv: [...o.execArgv] }) as never, join(res, "agent.js"), {
    onReady: (model, first) => { status.setModel(model, first); renderStatus(); },
    onReply: (id, text) => { recorder.runEnd(id); session.onReply(text); },
    onFailure: (id, code) => { if (id !== null) recorder.runEnd(id); if (code === "LLM_MODEL_NOT_FOUND") status.setModel(null, false); failure(code); },
    onUsage: (u) => recorder.request(u),
    onToolCall: async (name, input) => { const content = await session.onToolCall(name, input); return { content, stepPassed: session.consumeStepPassed() }; },
  });
  const runner = new SshCoreRunner({ targets, adminKey: () => keychain.get(serviceFor("app"), ADMIN_KEY_ACCOUNT), connectTimeoutMs: CONNECT_TIMEOUT_MS });
  const phrase = new PhraseActions({
    runner, box, renderKit, print: printHtml, lang: () => (locale === "ja" ? "ja" : "en"),
    label: t, emit: ui, onProceed: () => void session.refreshStatus(),
  });
  const session: Session = new Session({
    registry, refs: new PathRefTable(), keychain, service: () => serviceFor("repo"), t, approvalKey: await approvalKeyOf(keychain), runner,
    now: () => new Date().toISOString(), sendToAgent: (id, text) => { recorder.runStart(id); host.send(id, text); },
    onChat: (l) => { chat.push(l); renderChat(); },
    onCard: (card) => new Promise<CardDecision>((resolve) => { cardResolver.current = resolve; ui({ surface: "approval-card", card }); }),
    onPanel: (panel) => phrase.setPanel(panel),
    onSurvey: (r) => { survey.current = buildSurveyView(r, t, locale); renderStatus(); },
    secretBox: box, bootstrap: bootstrapRunner(b, keys, targets, keychain, typedFp), status, onStatus: renderStatus,
    judge: new StepJudge(b.byPath("install.md"), marksOf(b)),
  });
  const ctx: Ctx = { res, locale, keychain, box, host, session, phrase, targetUi, runner, label: t, registry, bundle: b, targets, cardResolver, typedFp, renderStatus };
  ctxRef.current = ctx;
  return ctx;
}
const ctxRef: { current: Ctx | null } = { current: null };

function bootstrapRunner(b: Extract<LoadedBundle, { ok: true }>, keys: TrustedKey[], targets: TargetStore, keychain: MacKeychain, typedFp: Ctx["typedFp"]): (input: Record<string, unknown>) => Promise<Record<string, unknown>> {
  const reader: BundleReader = {
    version: app.getVersion(),
    list: (prefix) => b.files.map((f) => f.path).filter((p) => p.startsWith(prefix)),
    read: (rel) => { const f = b.files.find((x) => x.path === rel); return f === undefined ? null : { bytes: f.bytes, sig: f.sig }; },
  };
  return (input) => runBootstrap(input as unknown as BootstrapInput, {
    targets, bundle: reader, keys,
    adminCredential: () => keychain.get(SERVICE_PRE_ADMIN, String(input.admin_ref)),
    clearAdminCredential: async () => { await keychain.delete(SERVICE_PRE_ADMIN, String(input.admin_ref)); },
    loadAdminKey: () => keychain.get(serviceFor("app"), ADMIN_KEY_ACCOUNT),
    saveAdminKey: (k) => keychain.set(serviceFor("app"), ADMIN_KEY_ACCOUNT, k),
    typedFingerprint: () => typedFp.current, connectTimeoutMs: CONNECT_TIMEOUT_MS,
  });
}

function registerIpc(ctx: Ctx): void {
  ipcMain.handle(CHANNEL.chatSend, (_e, text: unknown) => (typeof text === "string" ? ctx.session.submit(text) : undefined));
  ipcMain.handle(CHANNEL.chatAbort, () => ctx.host.abort());
  ipcMain.handle(CHANNEL.cardDecision, (_e, _id: unknown, d: CardDecision) => { ctx.cardResolver.current?.(d); ctx.cardResolver.current = null; });
  ipcMain.handle(CHANNEL.secretSubmit, (_e, ref: unknown, value: unknown) => (typeof ref === "string" && typeof value === "string" ? ctx.box.submit(ref, value) : undefined));
  ipcMain.handle(CHANNEL.phraseAction, (_e, a: unknown) => (a === "print" || a === "verify" || a === "confirm" || a === "handwritten" || a === "cancel" ? ctx.phrase.handle(a) : undefined));
  ipcMain.handle(CHANNEL.statusAction, (_e, kind: unknown) => { if (typeof kind === "string") ctx.session.askAi(kind); });
  ipcMain.handle(CHANNEL.targetRegister, async (_e, host: unknown, port: unknown, fp: unknown) => { if (ctx.targetUi.register({ host, port, fingerprint: fp }).ok) await ctx.session.refreshStatus(); });
  ipcMain.handle(CHANNEL.targetFingerprint, (_e, fp: unknown) => { ctx.targetUi.setFingerprint(fp); });
  ipcMain.handle(CHANNEL.targetRemove, () => { ctx.targetUi.remove(); });
}

/** エージェントを起動する。LLM API キーが無い(または差し替え)ときは、アプリ自身が開くモードの入力ボックスで受ける。 */
async function startAgent(ctx: Ctx, replace: boolean): Promise<void> {
  if (replace || (await ctx.keychain.get(SERVICE_PRE_LLM, LLM_ACCOUNT)) === null) {
    const out = await ctx.box.request("app-open", "llm", SECRET_LABEL.llm);
    if (out.kind === "cancelled") return;
  }
  const apiKey = await ctx.keychain.get(SERVICE_PRE_LLM, LLM_ACCOUNT);
  if (apiKey === null) return;
  const b = ctx.bundle;
  const agentBundle = { manual: b.byPath("manual.md"), procedure: b.byPath("install.md"), rules: b.paths("rules/").map(b.byPath).join("\n\n"), tools: ctx.registry.definitions(), models: (JSON.parse(b.byPath("models.json")) as { models: string[] }).models };
  ctx.host.start(apiKey, agentBundle, ctx.locale, new Date().toISOString().slice(0, 10));
}

/** 手順書の印({{名前}})の値。署名つきの marks.json があればそれ(無ければ値の無い印を含む期待は満たされない扱い)。 */
function marksOf(bundle: { paths: (p: string) => string[]; byPath: (p: string) => string }): Record<string, unknown> {
  return bundle.paths("marks.json").length > 0 ? (JSON.parse(bundle.byPath("marks.json")) as Record<string, unknown>) : {};
}

void main();
