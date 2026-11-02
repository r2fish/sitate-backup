// 会話の制御(主プロセス。spec 3.7、完了条件 5・8・22・26・31)。入力の送信前の検出、表示しなかった応答の区画、道具の呼び出しの検査と実行、
// 承認カードの操作待ち、道具の呼び出しの 1 行の表示。モデルへ出る文字列は shape-envelope を通ったものと人の入力だけ。
import { appErrorEnvelope } from "./app-envelope.ts";
import type { AppErrorCode } from "./app-envelope.ts";
import { buildCard } from "./approval-card.ts";
import type { CardModel, Translate } from "./approval-card.ts";
import { SoftwareApprovalKey } from "./approval-sign.ts";
import { guardOutgoing } from "./chat-guard.ts";
import { ingestCoreOutput } from "./core-output.ts";
import { SshError } from "./ssh/connection.ts";
import { UnsendableArgvError, newRunId } from "./ssh/core-runner.ts";
import { SECRET_LABEL } from "./secret-box.ts";
import type { SecretBox } from "./secret-box.ts";
import type { StatusView } from "./status-view.ts";
import type { StepJudge } from "./step-judge.ts";
import type { PhrasePanel } from "./phrase-panel.ts";
import type { CardDecision } from "./ipc-channels.ts";
import type { Keychain } from "./keychain.ts";
import { SERVICE_PRE_ADMIN } from "./keychain.ts";
import { PathRefTable } from "./path-ref.ts";
import { QuoteRejections, checkQuotes } from "./quote-check.ts";
import { shapeEnvelope, toDataSection } from "./shape-envelope.ts";
import type { StreamEnd } from "./stdout-records.ts";
import { authorizeToolCall, buildStdin } from "./tool-gate.ts";
import type { ToolPlan } from "./tool-gate.ts";
import type { ToolRegistry } from "./tool-registry.ts";

export interface CoreRunner { run(argv: readonly string[], stdin: string): Promise<{ stdout: string; end: StreamEnd }> }
type RunOutcome = { envelope: Record<string, unknown> } | { discarded: true; text: string; code: string };
export type ChatLine = { readonly role: "human" | "agent" | "tool" | "notice"; readonly text: string };

export interface SessionDeps {
  readonly registry: ToolRegistry;
  readonly refs: PathRefTable;
  readonly keychain: Keychain;
  readonly service: () => string;
  readonly runner: CoreRunner;
  readonly t: Translate;
  readonly approvalKey: SoftwareApprovalKey;
  readonly now: () => string;
  /** エージェントへ入力を渡す(id, text)。 */
  readonly sendToAgent: (id: string, text: string) => void;
  readonly onChat: (line: ChatLine) => void;
  readonly onCard: (card: CardModel) => Promise<CardDecision>;
  /** フレーズのパネルを開く。済む(done)か取り消される(cancelled)まで解けない(道具の呼び出しはそれまで戻らない。spec 3.7)。 */
  readonly onPanel: (panel: PhrasePanel) => Promise<"done" | "cancelled">;
  /** 秘密の入力ボックス(3 モード)。無ければ参照名がキーチェーンに無い呼び出しは HUMAN_CANCELLED になる。 */
  readonly secretBox?: SecretBox;
  /** `bootstrap`(アプリが SSH で実行し、封筒を作る)。管理アクセスの資格情報は呼び出しの前にキーチェーンに置かれている。 */
  readonly bootstrap?: (input: Record<string, unknown>) => Promise<Record<string, unknown>>;
  /** 手順の合格の判定(表示と往復の数のためだけに使う)。 */
  readonly judge?: StepJudge;
  /** 状態と報告の表示(`status` の読み直しと「AI に見立てを聞く」)。 */
  readonly status?: StatusView;
  /** 状態の表示が変わったとき(描画を促す)。 */
  readonly onStatus?: () => void;
  /** `source survey` が成功したとき、核の結果の全体(実名つき。モデルへは送らず、状態と報告の表示の区画にだけ描く)。 */
  readonly onSurvey?: (result: Record<string, unknown>) => void;
}

/** 事業者の管理用鍵を取る道具(spec 3.7 の写像): 保存しないモードで開く。 */
const BUSINESS_KEY_TOOLS = new Set(["sitate_init_storage", "sitate_init", "sitate_apply"]);
/** 元サーバーの管理用資格情報が通らなかったとき、その参照名の値をキーチェーンから消す(spec 3.7)。 */
const SOURCE_AUTH_TOOLS = new Set(["sitate_source_survey", "sitate_source_enroll"]);
const AUTH_REASONS = new Set(["auth", "key_unparsable"]);

const APP_CODES = new Set<string>(["BOOTSTRAP_SIGNATURE_INVALID", "BOOTSTRAP_PRIVILEGE_REQUIRED", "BOOTSTRAP_PLATFORM_UNSUPPORTED", "BOOTSTRAP_PACKAGE_FAILED", "BOOTSTRAP_ALREADY_DONE", "BOOTSTRAP_NOT_DONE", "BOOTSTRAP_RESTORE_ORDER", "TOOL_DENIED", "TOOL_ARG_INVALID", "PATH_REF_UNKNOWN", "HUMAN_CANCELLED", "QUOTE_REJECTED", "SSH_UNREACHABLE", "SSH_AUTH_DENIED", "SSH_HOSTKEY_MISMATCH", "SSH_DISCONNECTED", "ENVELOPE_INVALID", "ENVELOPE_SECRET_MISMATCH", "ENVELOPE_TOO_MANY_RECORDS", "ENVELOPE_EXIT_UNKNOWN"]);
const APP_KEY: Record<string, string> = { BOOTSTRAP_SIGNATURE_INVALID: "app.bootstrap.signature_invalid", BOOTSTRAP_PRIVILEGE_REQUIRED: "app.bootstrap.privilege_required", BOOTSTRAP_PLATFORM_UNSUPPORTED: "app.bootstrap.platform_unsupported", BOOTSTRAP_PACKAGE_FAILED: "app.bootstrap.package_failed", BOOTSTRAP_ALREADY_DONE: "app.bootstrap.already_done", BOOTSTRAP_NOT_DONE: "app.bootstrap.not_done", BOOTSTRAP_RESTORE_ORDER: "app.bootstrap.restore_order", TOOL_DENIED: "app.tool.denied", TOOL_ARG_INVALID: "app.tool.arg_invalid", PATH_REF_UNKNOWN: "app.tool.path_ref_unknown", HUMAN_CANCELLED: "app.tool.human_cancelled", QUOTE_REJECTED: "app.tool.quote_rejected", SSH_UNREACHABLE: "app.ssh.unreachable", SSH_AUTH_DENIED: "app.ssh.auth_denied", SSH_HOSTKEY_MISMATCH: "app.ssh.hostkey_mismatch", SSH_DISCONNECTED: "app.ssh.disconnected", ENVELOPE_INVALID: "app.envelope.invalid", ENVELOPE_SECRET_MISMATCH: "app.envelope.secret_mismatch", ENVELOPE_TOO_MANY_RECORDS: "app.envelope.too_many_records", ENVELOPE_EXIT_UNKNOWN: "app.envelope.exit_unknown" };

export class Session {
  readonly #d: SessionDeps;
  readonly #quotes = new QuoteRejections();
  readonly #results = new Map<string, unknown>();
  readonly #humanTexts: string[] = [];
  /** 事業者の管理用鍵のうちキーチェーンに置いたもの(init-storage の ok:true と続く bucket check の合格、またはアプリを閉じる時点で消す)。 */
  readonly #businessRefs = new Set<string>();
  #storageInitialized = false;
  /** この呼び出しで開いたフレーズのパネルの待ち。 */
  #panelWait: Promise<"done" | "cancelled"> | null = null;
  #newSteps: string[] = [];
  #stepPassed = false;
  #seq = 0;

  constructor(deps: SessionDeps) {
    this.#d = deps;
  }

  /** チャット欄の送信。秘密を検出したら送らず、入力を保持せず、固定文だけを出す。 */
  async submit(text: string): Promise<void> {
    const kc = await this.#keychainValues();
    const g = guardOutgoing(text, kc);
    if (!g.send) { for (const k of g.noticeKeys) this.#d.onChat({ role: "notice", text: this.#d.t(k) }); return; }
    this.#deliver(text);
  }

  #deliver(text: string): void {
    this.#humanTexts.push(text);
    this.#d.onChat({ role: "human", text });
    const block = this.#quotes.take();
    this.#d.sendToAgent(`i${this.#seq++}`, block === null ? text : `${block}\n${text}`);
  }

  /** `status` を読み直す(起動時・面を開いたとき・状態を変える操作の後)。届かなければ最後の値を古い印つきで残す。 */
  async refreshStatus(): Promise<void> {
    if (this.#d.status === undefined) return;
    const r = await this.#run(["status"], "");
    this.#d.status.ingest("envelope" in r && r.envelope.ok === true ? r.envelope : null);
    this.#d.onStatus?.();
  }

  /** 「AI に見立てを聞く」を押した。変化の種類ごとのカタログの固定文を、人の入力としてエージェントへ渡す(押さなければモデルを呼ばない)。 */
  askAi(kind: string): void {
    const text = this.#d.status?.press(kind) ?? null;
    if (text === null) return;
    this.#deliver(text);
    this.#d.onStatus?.();
  }

  onReply(text: string): void {
    const v = checkQuotes({ text, results: this.#results, humanTexts: this.#humanTexts });
    // 引用の照合は参照名のままの文で行い、画面にはエージェントの文の参照名を実名に置き換えて描く(完了条件 33。モデルへ戻らない)。
    if (v.ok) return this.#d.onChat({ role: "agent", text: this.#d.refs.renderText(text) });
    this.#quotes.record(v);
    this.#d.onChat({ role: "notice", text: this.#d.t("chat.quote.rejected_guide") });
  }

  async #keychainValues(): Promise<string[]> {
    const items = await this.#d.keychain.list(this.#d.service());
    const vals = await Promise.all(items.map((i) => this.#d.keychain.get(i.service, i.account)));
    return vals.filter((v): v is string => v !== null);
  }

  /**
   * 道具の呼び出し(AgentRuntime のコールバック)。戻り値はモデルへ返す区画。
   * 人の操作が要る道具(フレーズのパネル)では、その操作の完了か取り消しまで戻らない(取り消しは HUMAN_CANCELLED)。
   */
  async onToolCall(name: string, input: unknown): Promise<string> {
    this.#panelWait = null;
    let out: string;
    try {
      out = await this.#toolCall(name, input);
    } catch (e) {
      this.#panelWait = null;
      return this.#internalFailure(name);
    }
    const wait = this.#panelWait;
    this.#panelWait = null;
    if (wait !== null && (await wait) === "cancelled") return toDataSection(appErrorEnvelope("HUMAN_CANCELLED", ["app", "tool"]) as unknown as Record<string, unknown>);
    return out;
  }

  /** 想定外の例外(版違いの核・不正な出力など)で会話を止めない。固定文の封筒だけをモデルへ返す。 */
  #internalFailure(name: string): string {
    return this.#finish(name, toDataSection(appErrorEnvelope("ENVELOPE_INVALID", ["app", "tool"]) as unknown as Record<string, unknown>), "ENVELOPE_INVALID");
  }

  async #toolCall(name: string, input: unknown): Promise<string> {
    const auth = authorizeToolCall(this.#d.registry, { id: "", name, input }, this.#d.refs);
    if (!auth.ok) return this.#finish(name, toDataSection(auth.envelope as unknown as Record<string, unknown>), auth.envelope.error.code);
    const plan = this.#withRunId(auth.plan);
    if (plan.tool === "sitate_bootstrap") return this.#bootstrap(plan);
    const secrets = await this.#collectSecrets(plan);
    if (secrets === null) return this.#cancelled(name);
    const first = await this.#run(plan.argv, buildStdin(plan, secrets));
    if ("discarded" in first) return this.#finish(name, first.text, first.code);
    let env = first.envelope;
    if (name === "sitate_apply" && codeOf(env) === "HUMAN_SECRET_REQUIRED") {
      const again = await this.#applyWithBusinessKey(plan);
      if (again === null) return this.#cancelled(name);
      if ("discarded" in again) return this.#finish(name, again.text, again.code);
      env = again.envelope;
    }
    await this.#afterRun(plan, env);
    if (plan.tool === "sitate_source_survey" && env.ok === true && isRecord(env.result)) this.#d.onSurvey?.(env.result);
    if (plan.tool === "sitate_status") { this.#d.status?.ingest(env.ok === true ? env : null); this.#d.onStatus?.(); }
    const needsCard = (name === "sitate_propose" || name === "sitate_attest") && env.ok === true;
    if (!needsCard) return this.#finish(name, this.#shape(env), codeOf(env));
    return this.#cardFlow(name, env);
  }

  /** 状態を変える道具(定義に run_id を持つもの)には、無ければ主プロセスが --run-id を付ける(切断の後に同じ run_id で呼び直せる。spec 3.7)。 */
  #withRunId(plan: ToolPlan): ToolPlan {
    if (plan.runId !== null || plan.dryRun || plan.tool === "sitate_bootstrap" || !this.#d.registry.hasProperty(plan.tool, "run_id")) return plan;
    const id = newRunId();
    return { ...plan, runId: id, argv: [...plan.argv, "--run-id", id] };
  }

  #cancelled(name: string): string {
    return this.#finish(name, toDataSection(appErrorEnvelope("HUMAN_CANCELLED", ["app", "tool"]) as unknown as Record<string, unknown>), "HUMAN_CANCELLED");
  }

  /**
   * secret_refs の値を集める。キーチェーンに有ればそれ、無ければ入力ボックスを開く(人が閉じれば null)。
   * 事業者の管理用鍵(init-storage・init・apply の admin_ref)は保存しないモード、ほかは保存するモード(spec 3.7 の写像)。
   */
  async #collectSecrets(plan: ToolPlan): Promise<Record<string, unknown> | null> {
    const values: Record<string, unknown> = {};
    const adminRef = typeof plan.input.admin_ref === "string" ? plan.input.admin_ref : null;
    for (const ref of plan.secretRefs) {
      const business = ref === adminRef && BUSINESS_KEY_TOOLS.has(plan.tool);
      const have = await this.#d.keychain.get(this.#d.service(), ref);
      if (have !== null) { values[ref] = have; if (business) this.#businessRefs.add(ref); continue; }
      if (business && plan.tool === "sitate_apply") continue; // apply は HUMAN_SECRET_REQUIRED が返ったときに開く
      const v = await this.#ask(business ? "no-save" : "save", ref);
      if (v === null) return null;
      values[ref] = v;
    }
    return values;
  }

  async #ask(mode: "save" | "no-save", ref: string): Promise<string | null> {
    const box = this.#d.secretBox;
    if (box === undefined) return null;
    const out = await box.request(mode, ref, SECRET_LABEL.generic);
    if (out.kind === "cancelled") return null;
    if (out.kind === "transient") return out.value.take();
    return this.#d.keychain.get(this.#d.service(), ref);
  }

  async #applyWithBusinessKey(plan: ToolPlan): Promise<RunOutcome | null> {
    const ref = typeof plan.input.admin_ref === "string" ? plan.input.admin_ref : null;
    if (ref === null) return null;
    const v = await this.#ask("no-save", ref);
    if (v === null) return null;
    const argv = plan.argv.includes("--secrets-stdin") ? plan.argv : [...plan.argv, "--secrets-stdin"];
    return this.#run(argv, JSON.stringify({ [ref]: v }) + "\n");
  }

  /** 結果に応じた秘密の後始末: 事業者の管理用鍵の寿命、元サーバーの資格情報の失敗。 */
  async #afterRun(plan: ToolPlan, env: Record<string, unknown>): Promise<void> {
    if (plan.tool === "sitate_init_storage" && env.ok === true && !plan.dryRun) this.#storageInitialized = true;
    if (plan.tool === "sitate_bucket_check" && env.ok === true && this.#storageInitialized) await this.#dropBusinessKeys();
    if (SOURCE_AUTH_TOOLS.has(plan.tool) && env.ok === false) {
      const d = (env.error as { detail?: { step?: unknown; reason?: unknown } } | null)?.detail;
      const ref = plan.input.admin_ref;
      if (typeof ref === "string" && d?.step === "connect" && typeof d.reason === "string" && AUTH_REASONS.has(d.reason)) await this.#d.keychain.delete(this.#d.service(), ref);
    }
  }

  async #dropBusinessKeys(): Promise<void> {
    for (const ref of this.#businessRefs) await this.#d.keychain.delete(this.#d.service(), ref);
    this.#businessRefs.clear();
    this.#storageInitialized = false;
  }

  /** この呼び出しで手順が合格したか(往復の数え直しの合図)。読むと下ろす。 */
  consumeStepPassed(): boolean {
    const v = this.#stepPassed;
    this.#stepPassed = false;
    return v;
  }

  /** アプリを閉じるとき。事業者の管理用鍵をキーチェーンから消す。 */
  async dispose(): Promise<void> {
    await this.#dropBusinessKeys();
  }

  async #bootstrap(plan: ToolPlan): Promise<string> {
    const ref = typeof plan.input.admin_ref === "string" ? plan.input.admin_ref : null;
    if (this.#d.bootstrap === undefined || ref === null) return this.#cancelled(plan.tool);
    if ((await this.#d.keychain.get(SERVICE_PRE_ADMIN, ref)) === null) {
      const out = await this.#d.secretBox?.request("save", ref, SECRET_LABEL.admin, SERVICE_PRE_ADMIN);
      if (out === undefined || out.kind !== "stored") return this.#cancelled(plan.tool);
    }
    const env = await this.#d.bootstrap({ ...plan.input, ...(plan.runId !== null ? { run_id: plan.runId } : {}) });
    return this.#finish(plan.tool, this.#shape(env), codeOf(env));
  }

  async #run(argv: readonly string[], stdin: string): Promise<RunOutcome> {
    let out: Awaited<ReturnType<CoreRunner["run"]>>;
    try {
      out = await this.#d.runner.run(argv, stdin);
    } catch (e) {
      const code = e instanceof SshError ? e.failure.code : e instanceof UnsendableArgvError ? "TOOL_ARG_INVALID" : null;
      if (code === null) throw e;
      const detail = e instanceof SshError && e.failure.code === "SSH_UNREACHABLE" ? { reason: e.failure.reason } : {};
      const env = appErrorEnvelope(code, argv.slice(0, 2), detail) as unknown as Record<string, unknown>;
      return { discarded: true, text: toDataSection(shapeEnvelope(env, this.#d.refs)), code };
    }
    const r = ingestCoreOutput(out.stdout, argv.slice(0, 2), out.end);
    if (r.kind === "discarded") return { discarded: true, text: toDataSection(r.envelope as unknown as Record<string, unknown>), code: r.envelope.error.code };
    if (r.panel !== null) this.#panelWait = this.#d.onPanel(r.panel);
    return { envelope: r.envelope as Record<string, unknown> };
  }

  async #cardFlow(name: string, proposeEnv: Record<string, unknown>): Promise<string> {
    const result = proposeEnv.result as { change_id?: unknown } | null;
    const id = typeof result?.change_id === "string" ? result.change_id : null;
    if (id === null) return this.#finish(name, this.#shape(proposeEnv), codeOf(proposeEnv));
    const pending = await this.#run(["pending", "--id", id], "");
    const record = "envelope" in pending ? firstChange(pending.envelope) : null;
    if (record === null) return this.#finish(name, this.#shape(proposeEnv), codeOf(proposeEnv));
    const decision = await this.#d.onCard(buildCard(record, { t: this.#d.t }));
    if (decision.kind === "close") return this.#finish(name, this.#shape(proposeEnv), null);
    if (decision.kind === "cancel") {
      await this.#run(["cancel", "--id", id], "");
      return this.#finish(name, toDataSection(appErrorEnvelope("HUMAN_CANCELLED", ["app", "tool"]) as unknown as Record<string, unknown>), "HUMAN_CANCELLED");
    }
    const a = this.#d.approvalKey.signRecord(record, this.#d.now());
    const proofs = Object.values(decision.proofs);
    const argv = ["approve", "--id", id, "--signature", a.signature, "--signed-at", a.signedAt, ...(proofs.length > 0 ? ["--notify-proof-stdin"] : [])];
    const done = await this.#run(argv, proofs.length > 0 ? JSON.stringify(decision.proofs) + "\n" : "");
    if ("discarded" in done) return this.#finish(name, done.text, done.code);
    return this.#finish(name, this.#shape(done.envelope), codeOf(done.envelope));
  }

  #shape(env: Record<string, unknown>): string {
    const shaped = shapeEnvelope(env, this.#d.refs);
    this.#results.set(Array.isArray(shaped.command) ? shaped.command.join(" ") : "", shaped);
    if (this.#d.judge !== undefined) this.#newSteps.push(...this.#d.judge.judge(shaped));
    return toDataSection(shaped);
  }

  /** 道具の呼び出しの 1 行: 表示名と結果だけ(引数・参照名・パスは描かない)。モデルへ送らず、履歴・引用の照合の対象にしない。 */
  #finish(tool: string, text: string, code: string | null): string {
    const t = this.#d.t;
    const result = code === null ? t("chat.tool.display.result_ok") : APP_CODES.has(code) ? t(APP_KEY[code]!) : safe(() => t(`core.code.${code}`), t(APP_KEY["ENVELOPE_INVALID"]!));
    const toolName = safe(() => t(`chat.tool.display.name.${tool}`), tool);
    this.#d.onChat({ role: "tool", text: t("chat.tool.display.call_format", { tool_name: toolName, result }) });
    for (const step of this.#newSteps) this.#d.onChat({ role: "tool", text: t("chat.tool.display.step_passed", { step }) });
    if (this.#newSteps.length > 0) this.#stepPassed = true;
    this.#newSteps = [];
    return text;
  }
}

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const safe = (f: () => string, fallback: string): string => { try { return f(); } catch { return fallback; } };
const codeOf = (env: Record<string, unknown>): string | null => {
  const e = env.error as { code?: unknown } | null | undefined;
  return env.ok === false && typeof e?.code === "string" ? e.code : null;
};
function firstChange(env: Record<string, unknown>): Record<string, unknown> | null {
  const r = env.result as { changes?: unknown } | null;
  const c = Array.isArray(r?.changes) ? r.changes[0] : null;
  return typeof c === "object" && c !== null ? (c as Record<string, unknown>) : null;
}
export type { AppErrorCode };
