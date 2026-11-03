// 秘密の入力ボックスの 3 モード(spec 3.7、完了条件 4)。値はモデルにもエージェントのプロセスにも渡さない。
//  - save: 値をキーチェーンへ直接保存し、参照名だけを返す(値は戻り値に載せない)。
//  - app-open: アプリ自身が開く(エージェントの起動前と LLM API キーの差し替え)。LLM API キーをキーチェーンへ保存し、参照名を返さない。
//  - no-save: 値をキーチェーンにもディスクにも置かず、呼び出し 1 回の標準入力へ渡した後にメモリから消す。
import type { Keychain } from "./keychain.ts";
import { SERVICE_PRE_LLM } from "./keychain.ts";
import type { SecretMode } from "./ipc-channels.ts";

export const LLM_ACCOUNT = "app:llm-api-key";

/** 入力ボックスのラベルのカタログのキー。参照名(モデルが決める)は画面に出さず、アプリが決めた文だけを出す。 */
export const SECRET_LABEL = { generic: "chat.secret.label.generic", admin: "chat.secret.label.admin", llm: "chat.secret.label.llm" } as const;

/** 一度だけ取り出せる値。取り出すとバッファを 0 で潰す(JS の文字列は潰せないため、保持の間は Buffer で持つ)。 */
export class Transient {
  #buf: Buffer | null;
  constructor(value: string) {
    this.#buf = Buffer.from(value, "utf8");
  }

  take(): string | null {
    if (this.#buf === null) return null;
    const v = this.#buf.toString("utf8");
    this.#buf.fill(0);
    this.#buf = null;
    return v;
  }

  discard(): void {
    this.#buf?.fill(0);
    this.#buf = null;
  }
}

export type SecretOutcome =
  | { readonly kind: "stored"; readonly ref: string | null }
  | { readonly kind: "transient"; readonly value: Transient }
  | { readonly kind: "cancelled" };

export interface SecretRequest { readonly mode: SecretMode; readonly ref: string; readonly label: string; readonly service?: string | undefined }

interface Pending { readonly req: SecretRequest; readonly resolve: (o: SecretOutcome) => void }

export class SecretBox {
  readonly #keychain: Keychain;
  readonly #service: () => string;
  readonly #show: (req: SecretRequest | null) => void;
  readonly #queue: Pending[] = [];
  #current: Pending | null = null;

  constructor(deps: { keychain: Keychain; service: () => string; show: (req: SecretRequest | null) => void }) {
    this.#keychain = deps.keychain;
    this.#service = deps.service;
    this.#show = deps.show;
  }

  /** ボックスを開いて人の入力を待つ。同時に開くのは 1 つ(後続は待つ)。閉じられたら cancelled。 */
  request(mode: SecretMode, ref: string, label: string, service?: string): Promise<SecretOutcome> {
    return new Promise((resolve) => {
      this.#queue.push({ req: { mode, ref, label, service }, resolve });
      this.#next();
    });
  }

  #next(): void {
    if (this.#current !== null) return;
    const p = this.#queue.shift();
    if (p === undefined) return this.#show(null);
    this.#current = p;
    this.#show(p.req);
  }

  /** 画面からの提出。値は保存するモードならキーチェーンへ、保存しないモードなら Transient へ入れ、ここには残さない。 */
  async submit(ref: string, value: string): Promise<void> {
    const p = this.#current;
    if (p === null || p.req.ref !== ref) return;
    this.#current = null;
    let out: SecretOutcome;
    if (value === "") out = { kind: "cancelled" };
    else if (p.req.mode === "save") { await this.#keychain.set(p.req.service ?? this.#service(), ref, value); out = { kind: "stored", ref }; }
    else if (p.req.mode === "app-open") { await this.#keychain.set(SERVICE_PRE_LLM, LLM_ACCOUNT, value); out = { kind: "stored", ref: null }; }
    else out = { kind: "transient", value: new Transient(value) };
    p.resolve(out);
    this.#next();
  }

  cancel(): void {
    const p = this.#current;
    this.#current = null;
    p?.resolve({ kind: "cancelled" });
    this.#next();
  }
}
