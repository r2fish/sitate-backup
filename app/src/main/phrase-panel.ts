// フレーズのパネル(spec 3.7、完了条件 6・7)。`parseStdoutRecords` が返した秘密のレコードを直接受け、入力欄を持たず、
// `phrase verify` と `kit confirm` が済むまで次の手順へ進ませない。印刷の一時ファイルは作らない(HTML は呼び出し側のメモリの印刷経路へ渡す)。
// 24 語は 1 つのバッファに持ち、閉じるときに 0 で潰す。ログ・会話履歴・モデルの入力に載せる経路を持たない。画面へ送るのは、開いている間のフレーズのパネルの面(`ui:render`)だけ。
import type { KitFields, Lang, PhraseWords, RenderKit } from "../kit/types.ts";
import type { SecretStringRecord } from "./stdout-records.ts";

export type PanelState = "showing" | "closed";

export interface PanelView {
  /** 画面に出す 1〜24 の番号付きの語。パネルが開いている間だけ返す。 */
  readonly words: readonly { readonly n: number; readonly word: string }[];
}

export class PhrasePanel {
  #buf: Buffer | null;
  #count: number;
  #verified = false;
  #confirmed = false;
  readonly keyringVersion: number;

  /** 秘密のレコードを受け取る。受け取った後、呼び出し側は record.words を保持しない。 */
  constructor(secret: SecretStringRecord) {
    this.#buf = Buffer.from(secret.words.join(" "), "utf8");
    this.#count = secret.words.length;
    this.keyringVersion = secret.keyring_version;
  }

  get state(): PanelState {
    return this.#buf === null ? "closed" : "showing";
  }

  view(): PanelView {
    if (this.#buf === null) throw new Error("パネルは閉じている");
    return { words: this.#buf.toString("utf8").split(" ").map((word, i) => ({ n: i + 1, word })) };
  }

  markVerified(): void {
    this.#verified = true;
  }

  markKitConfirmed(): void {
    this.#confirmed = true;
  }

  get verified(): boolean {
    return this.#verified;
  }

  get kitConfirmed(): boolean {
    return this.#confirmed;
  }

  /** phrase verify と kit confirm の両方が済んだときだけ次の手順へ進める。 */
  get canProceed(): boolean {
    return this.#verified && this.#confirmed;
  }

  /** 印刷物を作る(描画部 U3b を型に合わせて呼ぶ)。HTML は返すだけで、ファイルに書かない。 */
  render(render: RenderKit, fields: KitFields, lang: Lang): ReturnType<RenderKit> {
    if (this.#buf === null) throw new Error("パネルは閉じている");
    const phrase = this.#buf.toString("utf8").split(" ") as unknown as PhraseWords;
    return render({ fields, phrase, lang });
  }

  /** 進めるときだけ閉じる。閉じたらバッファを潰す。 */
  close(): void {
    if (this.#buf !== null) this.#buf.fill(0);
    this.#buf = null;
    this.#count = 0;
  }

  /** 秘密を含めない(JSON.stringify・ログへの誤出力の対策)。 */
  toJSON(): string {
    return `[フレーズのパネル ${this.state}]`;
  }

  get wordCount(): number {
    return this.#count;
  }
}

/** 次の手順へ進めてよいか。進めてよいのは verify と confirm の両方が済み、パネルを閉じたとき。進ませる側はこの関数の true だけで動く。 */
export function advancePanel(panel: PhrasePanel): boolean {
  if (!panel.canProceed) return false;
  panel.close();
  return true;
}
