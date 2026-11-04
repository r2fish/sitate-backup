// 会話の制御(主プロセス。spec 3.7、完了条件 5・8・22・26・31)。入力の送信前の検出、表示しなかった応答の区画、道具の呼び出しの検査と実行、
// 承認カードの操作待ち、道具の呼び出しの 1 行の表示。モデルへ出る文字列は shape-envelope を通ったものと人の入力だけ。
import type { AppErrorCode } from "./app-envelope.ts";
import { appErrorSection, appSection } from "./app-section.ts";
import { renderAgentText } from "./agent-text.ts";
import type { Span } from "./agent-text.ts";
import { buildCard } from "./approval-card.ts";
import type { CardModel, Translate } from "./approval-card.ts";
import { SoftwareApprovalKey } from "./approval-sign.ts";
import { guardOutgoing } from "./chat-guard.ts";
import { ingestCoreOutput } from "./core-output.ts";
import { SshError } from "./ssh/connection.ts";
import { UnsendableArgvError, newRunId } from "./ssh/core-runner.ts";
import { SECRET_LABEL } from "./secret-box.ts";
import { askSecret, collectSecrets } from "./secret-flow.ts";
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
import { authorizeToolCall, buildStdin, subcommandOf } from "./tool-gate.ts";
import type { ToolPlan } from "./tool-gate.ts";
import type { ToolRegistry } from "./tool-registry.ts";

export interface CoreRunner { run(argv: readonly string[], stdin: string): Promise<{ stdout: string; end: StreamEnd }> }
type RunOutcome = { envelope: Record<string, unknown> } | { discarded: true; text: string; code: string };
export type ChatLine = {
  readonly role: "human" | "agent" | "tool" | "notice";
  readonly text: string;
  /** エージェントの文に入れた値(参照名から戻した名前と引用の印の値)の範囲。画面は書式の対象にしない。 */
  readonly spans?: readonly Span[];
  /** 手順の合格の行(道具の行の畳みの件数に数えない)。 */
  readonly step?: true;
};

/** 手順の骨格(spec 3.8 の手順 1〜13)。 */
export const STEP_NUMBERS = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13] as const;
export type StepState = "passed" | "next" | "todo";
export interface ProgressRow { readonly n: number; readonly label: string; readonly state: StepState }

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
  /** 「始める」を受けてよいか(接続先の記録が無いとき真)。主プロセスの側で確かめる(描き直しの前の連打や、表示の古さに頼らない)。無ければ人の発話の有無だけで決める。 */
  readonly canStart?: () => boolean;
  /** 状態の表示が変わったとき(描画を促す)。 */
  readonly onStatus?: () => void;
  /** `source survey` が成功したとき、核の結果の全体(実名つき。モデルへは送らず、状態と報告の表示の区画にだけ描く)。 */
  readonly onSurvey?: (result: Record<string, unknown>) => void;
  /** 引用した手順書・説明書の本文(引用の照合で、これに出る値を引いた文は表示してよい。spec 3.7 の例外)。 */
  readonly citedBodies?: () => readonly string[];
  /** 応答の待ちの数が変わったとき(描画を促す)。 */
  readonly onWait?: (n: number) => void;
}

/** 元サーバーの管理用資格情報が通らなかったとき、その参照名の値をキーチェーンから消す(spec 3.7)。 */
const SOURCE_AUTH_TOOLS = new Set(["sitate_source_survey", "sitate_source_enroll"]);
const AUTH_REASONS = new Set(["auth", "key_unparsable"]);

const APP_CODES = new Set<string>(["BOOTSTRAP_SIGNATURE_INVALID", "BOOTSTRAP_PRIVILEGE_REQUIRED", "BOOTSTRAP_PLATFORM_UNSUPPORTED", "BOOTSTRAP_PACKAGE_FAILED", "BOOTSTRAP_ALREADY_DONE", "BOOTSTRAP_NOT_DONE", "BOOTSTRAP_RESTORE_ORDER", "TOOL_DENIED", "TOOL_ARG_INVALID", "PATH_REF_UNKNOWN", "HUMAN_CANCELLED", "QUOTE_REJECTED", "SSH_UNREACHABLE", "SSH_AUTH_DENIED", "SSH_HOSTKEY_MISMATCH", "SSH_DISCONNECTED", "ENVELOPE_INVALID", "ENVELOPE_SECRET_MISMATCH", "ENVELOPE_TOO_MANY_RECORDS", "ENVELOPE_EXIT_UNKNOWN"]);
const APP_KEY: Record<string, string> = { BOOTSTRAP_SIGNATURE_INVALID: "app.bootstrap.signature_invalid", BOOTSTRAP_PRIVILEGE_REQUIRED: "app.bootstrap.privilege_required", BOOTSTRAP_PLATFORM_UNSUPPORTED: "app.bootstrap.platform_unsupported", BOOTSTRAP_PACKAGE_FAILED: "app.bootstrap.package_failed", BOOTSTRAP_ALREADY_DONE: "app.bootstrap.already_done", BOOTSTRAP_NOT_DONE: "app.bootstrap.not_done", BOOTSTRAP_RESTORE_ORDER: "app.bootstrap.restore_order", TOOL_DENIED: "app.tool.denied", TOOL_ARG_INVALID: "app.tool.arg_invalid", PATH_REF_UNKNOWN: "app.tool.path_ref_unknown", HUMAN_CANCELLED: "app.tool.human_cancelled", QUOTE_REJECTED: "app.tool.quote_rejected", SSH_UNREACHABLE: "app.ssh.unreachable", SSH_AUTH_DENIED: "app.ssh.auth_denied", SSH_HOSTKEY_MISMATCH: "app.ssh.hostkey_mismatch", SSH_DISCONNECTED: "app.ssh.disconnected", ENVELOPE_INVALID: "app.envelope.invalid", ENVELOPE_SECRET_MISMATCH: "app.envelope.secret_mismatch", ENVELOPE_TOO_MANY_RECORDS: "app.envelope.too_many_records", ENVELOPE_EXIT_UNKNOWN: "app.envelope.exit_unknown" };

export class Session {
  readonly #d: SessionDeps;
  readonly #quotes = new QuoteRejections();
  /** エージェントへ渡してまだ結果を受けていない入力の数(spec 3.7「応答の待ち」)。 */
  #waiting = 0;
  readonly #results = new Map<string, unknown>();
  readonly #humanTexts: string[] = [];
  /** この呼び出しで開いたフレーズのパネルの待ち。 */
  #panelWait: Promise<"done" | "cancelled"> | null = null;
  #chain: Promise<void> = Promise.resolve();
  #newSteps: string[] = [];
  #stepPassed = false;
  #seq = 0;
  #started = false;

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

  /** 最初の一歩の「始める」(spec 3.7。D-0169): カタログの固定の指示を、人の発話として描いてエージェントへ送る(秘密の検出は通さない固定文)。 */
  start(): void {
    if (this.#started || this.#humanTexts.length > 0 || this.#d.canStart?.() === false) return;
    this.#started = true;
    this.#deliver(this.#d.t("chat.start.instruction"));
    this.#d.onStatus?.();
  }

  /** 応答の待ちの数。 */
  get waiting(): number {
    return this.#waiting;
  }

  #setWaiting(n: number): void {
    const next = Math.max(0, n);
    if (next === this.#waiting) return;
    this.#waiting = next;
    this.#d.onWait?.(next);
  }

  /** 入力 1 つの結果を受けた(表示した文・表示しなかった応答・失敗の固定文・固定文を出さない中断)。 */
  #settled(): void {
    this.#setWaiting(this.#waiting - 1);
  }

  /** エージェントの失敗。入力の id があればその入力の結果、無ければ(どの入力か分からない失敗・プロセスの終了)待ちを 0 に戻す。 */
  onAgentFailure(id: string | null): void {
    if (id === null) this.#setWaiting(0);
    else this.#settled();
  }

  /** エージェントの作り直し(LLM API キーの差し替えを含む)。待ちを 0 に戻す。 */
  onAgentRestart(): void {
    this.#setWaiting(0);
  }

  /** 会話を捨てる。手順の合格・導入の一覧・応答の待ちを捨てる(アプリの再起動の後の会話には出さない)。 */
  discardConversation(): void {
    this.#started = false;
    this.#d.judge?.reset();
    this.#setWaiting(0);
    this.#d.onStatus?.();
  }

  /** 導入の進み具合(手順 1〜13)。「始める」を押した会話の間だけ。合格していない最小の番号が「次」。 */
  progress(): readonly ProgressRow[] | null {
    if (!this.#started || this.#d.judge === undefined) return null;
    const judge = this.#d.judge;
    const next = STEP_NUMBERS.find((n) => !judge.isPassed(String(n)));
    return STEP_NUMBERS.map((n) => ({ n, label: this.#d.t(`status.progress.step.${n}`), state: judge.isPassed(String(n)) ? "passed" as const : n === next ? "next" as const : "todo" as const }));
  }

  #deliver(text: string): void {
    this.#humanTexts.push(text);
    this.#d.onChat({ role: "human", text });
    const block = this.#quotes.take(this.#d.refs);
    this.#setWaiting(this.#waiting + 1);
    this.#d.sendToAgent(`i${this.#seq++}`, block === null ? text : `${block}\n${text}`);
  }

  /** `status` を読み直す(起動時・面を開いたとき・状態を変える操作の後)。届かなければ最後の値を古い印つきで残す。 */
  async refreshStatus(): Promise<void> {
    if (this.#d.status === undefined) return;
    const r = await this.#run(["status"], ["status"], "");
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
    this.#showReply(text);
    this.#settled(); // 表示してから減らす(待ちの印が文より先に消えない)
  }

  #showReply(text: string): void {
    const v = checkQuotes({ text, results: this.#results, humanTexts: this.#humanTexts, citedBodies: this.#d.citedBodies?.() ?? [] });
    // 引用の照合は参照名のままの文で行い、画面にはエージェントの文の参照名を実名に置き換えて描く(完了条件 33。モデルへ戻らない)。
    if (v.ok) { const r = renderAgentText(text, (k) => this.#d.t(k), this.#d.refs); return this.#d.onChat({ role: "agent", text: r.text, spans: r.spans }); }
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
  onToolCall(name: string, input: unknown): Promise<string> {
    // 1 つの応答に道具の呼び出しが複数あっても並列に走らせない(承認カード・入力ボックス・パネルは同時に 1 つ。spec 3.7)。
    const run = this.#chain.then(() => this.#serial(name, input));
    this.#chain = run.then(() => undefined, () => undefined);
    return run;
  }

  async #serial(name: string, input: unknown): Promise<string> {
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
    if (wait !== null && (await wait) === "cancelled") return appErrorSection("HUMAN_CANCELLED", subcommandOf(name), this.#d.refs);
    return out;
  }

  /** 想定外の例外(版違いの核・不正な出力など)で会話を止めない。固定文の封筒だけをモデルへ返す。 */
  #internalFailure(name: string): string {
    return this.#finish(name, appErrorSection("ENVELOPE_INVALID", subcommandOf(name), this.#d.refs), "ENVELOPE_INVALID");
  }

  async #toolCall(name: string, input: unknown): Promise<string> {
    const auth = authorizeToolCall(this.#d.registry, { id: "", name, input }, this.#d.refs, { approvalPubkey: this.#d.approvalKey.sshPublicKeyBase64() });
    if (!auth.ok) return this.#finish(name, appSection(auth.envelope, this.#d.refs), auth.envelope.error.code);
    const plan = this.#withRunId(auth.plan);
    if (plan.tool === "sitate_bootstrap") return this.#bootstrap(plan);
    const secrets = await collectSecrets({ keychain: this.#d.keychain, service: this.#d.service, secretBox: this.#d.secretBox }, plan);
    if (secrets === null) return this.#cancelled(name);
    const first = await this.#run(plan.subcommand, plan.argv, buildStdin(plan, secrets));
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
    return this.#finish(name, appErrorSection("HUMAN_CANCELLED", subcommandOf(name), this.#d.refs), "HUMAN_CANCELLED");
  }

  async #applyWithBusinessKey(plan: ToolPlan): Promise<RunOutcome | null> {
    const ref = typeof plan.input.admin_ref === "string" ? plan.input.admin_ref : null;
    if (ref === null) return null;
    const v = await askSecret({ keychain: this.#d.keychain, service: this.#d.service, secretBox: this.#d.secretBox }, plan.tool, "admin_ref", ref, "no-save");
    if (v === null) return null;
    const argv = plan.argv.includes("--secrets-stdin") ? plan.argv : [...plan.argv, "--secrets-stdin"];
    return this.#run(plan.subcommand, argv, JSON.stringify({ [ref]: v }) + "\n");
  }

  /** 結果に応じた秘密の後始末: 事業者の管理用鍵の寿命、元サーバーの資格情報の失敗。 */
  async #afterRun(plan: ToolPlan, env: Record<string, unknown>): Promise<void> {
    if (SOURCE_AUTH_TOOLS.has(plan.tool) && env.ok === false) {
      const d = (env.error as { detail?: { step?: unknown; reason?: unknown } } | null)?.detail;
      const ref = plan.input.admin_ref;
      if (typeof ref === "string" && d?.step === "connect" && typeof d.reason === "string" && AUTH_REASONS.has(d.reason)) await this.#d.keychain.delete(this.#d.service(), ref);
    }
  }

  /** この呼び出しで手順が合格したか(往復の数え直しの合図)。読むと下ろす。 */
  consumeStepPassed(): boolean {
    const v = this.#stepPassed;
    this.#stepPassed = false;
    return v;
  }

  /** アプリを閉じるとき。事業者の管理用鍵は保存しないモードで受けるので、キーチェーンに残るものは無い(D-0159)。 */
  async dispose(): Promise<void> {
    await Promise.resolve();
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

  /** command は道具に当たるサブコマンドの語(引数・旗を含めない)。アプリが作る封筒の command になる。 */
  async #run(command: readonly string[], argv: readonly string[], stdin: string): Promise<RunOutcome> {
    let out: Awaited<ReturnType<CoreRunner["run"]>>;
    try {
      out = await this.#d.runner.run(argv, stdin);
    } catch (e) {
      const code = e instanceof SshError ? e.failure.code : e instanceof UnsendableArgvError ? "TOOL_ARG_INVALID" : null;
      if (code === null) throw e;
      const detail = e instanceof SshError && e.failure.code === "SSH_UNREACHABLE" ? { reason: e.failure.reason } : {};
      return { discarded: true, text: appErrorSection(code, command, this.#d.refs, detail), code };
    }
    const r = ingestCoreOutput(out.stdout, command, out.end);
    if (r.kind === "discarded") return { discarded: true, text: appSection(r.envelope, this.#d.refs), code: r.envelope.error.code };
    if (r.panel !== null) this.#panelWait = this.#d.onPanel(r.panel);
    return { envelope: r.envelope as Record<string, unknown> };
  }

  async #cardFlow(name: string, proposeEnv: Record<string, unknown>): Promise<string> {
    const result = proposeEnv.result as { change_id?: unknown } | null;
    const id = typeof result?.change_id === "string" ? result.change_id : null;
    if (id === null) return this.#finish(name, this.#shape(proposeEnv), codeOf(proposeEnv));
    const pending = await this.#run(["pending"], ["pending", "--id", id], "");
    const record = "envelope" in pending ? firstChange(pending.envelope) : null;
    if (record === null) return this.#finish(name, this.#shape(proposeEnv), codeOf(proposeEnv));
    const card = buildCard(record, { t: this.#d.t });
    const raw = await this.#d.onCard(card);
    // 承認できないカード(ハッシュの不一致・未知の欄)への承認は、画面の操作が何であれ閉じるとして扱う(主プロセスでも確かめる)。
    const decision: CardDecision = raw.kind === "approve" && !card.approveEnabled ? { kind: "close" } : raw;
    if (decision.kind === "close") return this.#finish(name, this.#shape(proposeEnv), null);
    if (decision.kind === "cancel") {
      await this.#run(["cancel"], ["cancel", "--id", id], "");
      return this.#finish(name, appErrorSection("HUMAN_CANCELLED", subcommandOf(name), this.#d.refs), "HUMAN_CANCELLED");
    }
    const a = this.#d.approvalKey.signRecord(record, this.#d.now());
    const proofs = Object.values(decision.proofs);
    const argv = ["approve", "--id", id, "--signature", a.signature, "--signed-at", a.signedAt, ...(proofs.length > 0 ? ["--notify-proof-stdin"] : [])];
    const done = await this.#run(["approve"], argv, proofs.length > 0 ? JSON.stringify(decision.proofs) + "\n" : "");
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
    for (const step of this.#newSteps) this.#d.onChat({ role: "tool", text: t("chat.tool.display.step_passed", { step }), step: true });
    if (this.#newSteps.length > 0) { this.#stepPassed = true; this.#d.onStatus?.(); }
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
