// フレーズのパネルの操作(spec 3.7、完了条件 7): 印刷(または手書き) → 再入力で `phrase verify` → 2 か所保管で `kit confirm`。
// パネルには入力欄が無い。再入力は秘密の入力ボックスの保存しないモードで受け、呼び出し 1 回の標準入力へ渡して消す。
// 24 語は再入力の入力ボックスを開く時点でメモリから潰し、再表示しない(写し間違いのときは `init` のやり直し)。印刷の HTML は渡すだけで、ファイルに書かない。
// パネルの道具の呼び出し(`init` など)は、このパネルが済む(done)か取り消される(cancelled)まで戻らない(setPanel の戻り値。spec 3.7)。
import type { KitFields, Lang, RenderKit } from "../kit/types.ts";
import { ingestCoreOutput } from "./core-output.ts";
import type { PhraseAction } from "./ipc-channels.ts";
import { advancePanel } from "./phrase-panel.ts";
import type { PhrasePanel } from "./phrase-panel.ts";
import type { SecretBox } from "./secret-box.ts";
import { SshError } from "./ssh/connection.ts";
import type { StreamEnd } from "./stdout-records.ts";

export type PanelOutcome = "done" | "cancelled";

export interface PanelCoreRunner { run(argv: readonly string[], stdin: string): Promise<{ stdout: string; end: StreamEnd }> }

/** フレーズのパネルの面へ送る内容。words が null なら語は出さない。open が false なら閉じる。 */
export interface PanelMessage {
  readonly surface: "phrase-panel";
  readonly open: boolean;
  readonly words: readonly { readonly n: number; readonly word: string }[] | null;
  readonly canProceed: boolean;
  readonly actions: readonly { readonly action: PhraseAction; readonly label: string }[];
  readonly notice: string | null;
  /** 撮影の注意と保管場所の確認(spec 3.7)。パネルが開いている間だけ出す。 */
  readonly cautions: readonly string[];
}

export interface PhraseActionDeps {
  readonly runner: PanelCoreRunner;
  readonly box: SecretBox;
  /** 描画部(U3b)。無ければ印刷は固定文で断り、kit confirm にも進めない。 */
  readonly renderKit: RenderKit | null;
  /** HTML を OS の印刷へ渡す(メモリのまま。ファイルを作らない)。 */
  readonly print: (html: string) => Promise<void>;
  readonly lang: () => Lang;
  /** カタログの文(キーはカタログに全てある。欠けは試験で赤になる)。 */
  readonly label: (key: string, args?: Record<string, string | number>) => string;
  readonly emit: (m: PanelMessage) => void;
  /** 次の手順へ進めたとき(状態を読み直す)。 */
  readonly onProceed: () => void;
}

const REF_PHRASE = "phrase-verify";
/** 再入力の入力ボックスのラベル(カタログのキー)。 */
export const PHRASE_INPUT_LABEL_KEY = "phrase.verify.input_label";
const CODE_RE = /^[A-Z][A-Z0-9_]{0,63}$/;
const APP_CODE_KEYS: Readonly<Record<string, string>> = {
  SSH_UNREACHABLE: "app.ssh.unreachable", SSH_AUTH_DENIED: "app.ssh.auth_denied", SSH_HOSTKEY_MISMATCH: "app.ssh.hostkey_mismatch", SSH_DISCONNECTED: "app.ssh.disconnected",
  ENVELOPE_INVALID: "app.envelope.invalid", ENVELOPE_SECRET_MISMATCH: "app.envelope.secret_mismatch", ENVELOPE_TOO_MANY_RECORDS: "app.envelope.too_many_records", ENVELOPE_EXIT_UNKNOWN: "app.envelope.exit_unknown",
};
const rec = (v: unknown): Record<string, unknown> | null => (typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : null);

export class PhraseActions {
  readonly #d: PhraseActionDeps;
  #panel: PhrasePanel | null = null;
  #waiter: ((o: PanelOutcome) => void) | null = null;
  #busy = false;
  #printedVersion: string | null = null;
  #handwritten = false;

  constructor(deps: PhraseActionDeps) {
    this.#d = deps;
  }

  /**
   * 核の出力から取り出したパネルを受ける。前のパネルが残っていれば潰し、その待ちは cancelled で解く。
   * 戻り値は、このパネルが済む(verify と confirm)と done、取り消される(やめる・潰される・アプリを閉じる)と cancelled で解ける。
   */
  setPanel(panel: PhrasePanel): Promise<PanelOutcome> {
    this.#end("cancelled");
    this.#panel = panel;
    this.#printedVersion = null;
    this.#handwritten = false;
    const wait = new Promise<PanelOutcome>((resolve) => { this.#waiter = resolve; });
    this.#show(null);
    return wait;
  }

  /** 開いているパネルを潰し、待ちを解く。 */
  #end(outcome: PanelOutcome): void {
    this.#panel?.close();
    this.#panel = null;
    const w = this.#waiter;
    this.#waiter = null;
    w?.(outcome);
  }

  /** アプリを閉じるとき。 */
  dispose(): void {
    this.#end("cancelled");
  }

  async handle(action: PhraseAction): Promise<void> {
    const p = this.#panel;
    if (p === null) return;
    if (action === "cancel") { this.#end("cancelled"); return this.#show(null); }
    if (this.#busy) return;
    this.#busy = true;
    try {
      if (action === "print") await this.#print(p);
      else if (action === "handwritten") this.#markHandwritten(p);
      else if (action === "verify") await this.#verify(p);
      else if (action === "confirm") await this.#confirm(p);
    } finally {
      this.#busy = false;
    }
  }

  #actions(p: PhrasePanel): PanelMessage["actions"] {
    const l = this.#d.label;
    const cancel = { action: "cancel" as const, label: l("phrase.action.cancel") };
    if (p.verified) return p.kitConfirmed ? [cancel] : [{ action: "confirm", label: l("phrase.action.confirm") }, cancel];
    const copied = this.#printedVersion !== null || this.#handwritten;
    const out: { action: PhraseAction; label: string }[] = [];
    if (p.state === "showing") out.push({ action: "print", label: l("phrase.action.print") });
    if (p.state === "showing" && !copied) out.push({ action: "handwritten", label: l("phrase.action.handwritten") });
    if (copied || p.state === "closed") out.push({ action: "verify", label: l("phrase.action.verify") });
    return [...out, cancel];
  }

  #show(notice: string | null): void {
    const p = this.#panel;
    if (p === null) return this.#d.emit({ surface: "phrase-panel", open: false, words: null, canProceed: false, actions: [], notice, cautions: [] });
    const l = this.#d.label;
    this.#d.emit({
      surface: "phrase-panel", open: true, words: p.state === "showing" ? p.view().words : null, canProceed: p.canProceed, actions: this.#actions(p), notice,
      cautions: p.state === "showing" ? [l("phrase.photo_caution"), l("phrase.storage_check")] : [l("phrase.storage_check")],
    });
  }

  async #core(argv: readonly string[], stdin: string): Promise<{ ok: boolean; result: Record<string, unknown> | null; code: string | null }> {
    let out: Awaited<ReturnType<PanelCoreRunner["run"]>>;
    try {
      out = await this.#d.runner.run(argv, stdin);
    } catch (e) {
      return { ok: false, result: null, code: e instanceof SshError ? e.failure.code : "SSH_UNREACHABLE" };
    }
    const r = ingestCoreOutput(out.stdout, argv, out.end);
    if (r.kind === "discarded") return { ok: false, result: null, code: r.envelope.error.code };
    const env = r.envelope as unknown as Record<string, unknown>;
    r.panel?.close(); // この口で秘密のレコードは期待しない。来たら捨てる
    const raw = env.ok === true ? null : rec(env.error)?.code;
    const code = raw === null ? null : typeof raw === "string" && CODE_RE.test(raw) ? raw : "ENVELOPE_INVALID";
    return { ok: env.ok === true, result: rec(env.result), code };
  }

  /** 原因コードの固定文。核が返した形の外のコードや、カタログに無いコードは「応答の形が不正」の文に寄せる(核の文字列をそのまま出さない)。 */
  #codeText(code: string): string {
    const key = Object.hasOwn(APP_CODE_KEYS, code) ? APP_CODE_KEYS[code]! : `core.code.${code}`;
    try {
      return this.#d.label(key);
    } catch {
      return this.#d.label("app.envelope.invalid");
    }
  }

  #markHandwritten(p: PhrasePanel): void {
    if (p.state !== "showing") return;
    this.#handwritten = true;
    this.#show(null);
  }

  async #print(p: PhrasePanel): Promise<void> {
    const render = this.#d.renderKit;
    if (p.state !== "showing") return;
    if (render === null) return this.#show(this.#d.label("phrase.print.unavailable"));
    const kit = await this.#core(["kit"], "");
    const fields = kit.ok ? (kit.result?.fields as KitFields | undefined) : undefined;
    const version = kit.ok ? kit.result?.kit_version : undefined;
    if (fields === undefined || typeof version !== "string") return this.#show(this.#codeText(kit.code ?? "ENVELOPE_INVALID"));
    let html: string;
    try {
      const out = p.render(render, fields, this.#d.lang());
      if (!out.ok) return this.#show(this.#d.label("phrase.print.does_not_fit"));
      html = out.printable.html;
      await this.#d.print(html);
    } catch {
      return this.#show(this.#d.label("phrase.print.failed"));
    }
    if (this.#panel !== p) return;
    this.#printedVersion = version; // 紙にした版。kit confirm はこの版で記録する
    this.#show(null);
  }

  async #verify(p: PhrasePanel): Promise<void> {
    if (this.#printedVersion === null && !this.#handwritten && p.state === "showing") return;
    p.close(); // 再入力の入力ボックスを開く時点で 24 語を消す(再表示しない。写し間違いは init のやり直し)
    this.#show(null);
    const got = await this.#d.box.request("no-save", REF_PHRASE, PHRASE_INPUT_LABEL_KEY);
    if (this.#panel !== p) { if (got.kind === "transient") got.value.discard(); return; }
    if (got.kind !== "transient") return this.#show(null);
    const text = got.value.take();
    if (text === null || text.trim() === "") return this.#show(null);
    const phrase = text.trim().split(/\s+/).join(" ");
    const r = await this.#core(["phrase", "verify", "--phrase-stdin"], phrase + "\n");
    if (this.#panel !== p) return;
    if (r.ok && r.result?.verified === true) {
      p.markVerified();
      return this.#show(null);
    }
    this.#show(this.#codeText(r.code ?? "PHRASE_INVALID"));
  }

  async #confirm(p: PhrasePanel): Promise<void> {
    if (!p.verified) return;
    const version = this.#printedVersion;
    if (version === null) return this.#show(this.#d.label("phrase.confirm.needs_print")); // 印刷が成功するまで保管の確認に進めない
    const r = await this.#core(["kit", "confirm", "--kit-version", version, "--copies", "2"], "");
    if (this.#panel !== p) return;
    if (!r.ok) return this.#show(this.#codeText(r.code ?? "ENVELOPE_INVALID"));
    p.markKitConfirmed();
    if (advancePanel(p)) {
      this.#panel = null;
      const w = this.#waiter;
      this.#waiter = null;
      this.#show(null);
      w?.("done");
      this.#d.onProceed();
    }
  }
}
