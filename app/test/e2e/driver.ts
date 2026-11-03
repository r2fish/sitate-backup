// 試験用ドライバのクライアント(完了条件 12)。試験用のアプリ(SITATE_TEST_DRIVER_SOCKET の UNIX ソケットを開く)に接続し、5 面を操作する。
// 受入試験の A2・A6・A7 の画面の操作はここから駆動する。製品のアプリには含めない。
import { StringDecoder } from "node:string_decoder";
import { connect } from "node:net";
import type { Socket } from "node:net";
import type { DriverCommand, TestEvent } from "../../src/testing/driver-server.ts";

export const SURFACES = ["chat", "approval-card", "secret-box", "phrase-panel", "status"] as const;
export type SurfaceName = (typeof SURFACES)[number];
export interface PendingView {
  readonly secret: string | null;
  readonly card: { readonly proofs: string[]; readonly actions: { action: string; disabled: boolean }[]; readonly text: string } | null;
  readonly phrase: { readonly words: string[]; readonly actions: string[]; readonly notice: string | null } | null;
}
type Reply = { result?: unknown; error?: string };

export class Driver {
  readonly #sock: Socket;
  #buf = "";
  readonly #waiters: ((r: Reply) => void)[] = [];

  constructor(sock: Socket) {
    this.#sock = sock;
    // 受信のチャンクの境目が UTF-8 の文字の途中に当たっても壊さない(d.toString だとその文字が U+FFFD になる)。
    const decoder = new StringDecoder("utf8");
    sock.on("data", (d: Buffer) => {
      this.#buf += decoder.write(d);
      for (let nl = this.#buf.indexOf("\n"); nl >= 0; nl = this.#buf.indexOf("\n")) {
        const line = this.#buf.slice(0, nl);
        this.#buf = this.#buf.slice(nl + 1);
        this.#waiters.shift()?.(JSON.parse(line) as Reply);
      }
    });
  }

  static connect(socketPath: string): Promise<Driver> {
    return new Promise((resolve, reject) => {
      const s = connect(socketPath, () => resolve(new Driver(s)));
      s.on("error", reject);
    });
  }

  async #send(c: DriverCommand): Promise<unknown> {
    const p = new Promise<Reply>((r) => this.#waiters.push(r));
    this.#sock.write(JSON.stringify(c) + "\n");
    const r = await p;
    if (r.error !== undefined) throw new Error(r.error);
    return r.result;
  }

  /** 表示言語を切り替える(イテレーション 1 は ja。en はイテレーション 3)。 */
  setLanguage(lang: "ja" | "en"): Promise<unknown> { return this.#send({ op: "lang", lang }); }
  /** チャット欄に入力して送信する。 */
  async chat(text: string): Promise<void> { await this.#send({ op: "chat", text }); }
  /** 面の操作(承認カードの承認・取り消し・閉じる、フレーズのパネルの verify・confirm など)。押せなければ false。 */
  async click(surface: SurfaceName, action: string): Promise<boolean> { return (await this.#send({ op: "click", surface, action })) === true; }
  /** 欄に入れる(確認語の入力欄、秘密の入力ボックス)。 */
  async fill(surface: SurfaceName, target: string, value: string): Promise<boolean> { return (await this.#send({ op: "fill", surface, target, value })) === true; }
  /** 面の表示文字列。 */
  async read(surface: SurfaceName): Promise<string | null> { return (await this.#send({ op: "read", surface })) as string | null; }
  /** チャット欄の行(役割: human / agent / tool / notice)。 */
  async lines(): Promise<{ role: string; text: string }[]> { return (await this.#send({ op: "lines" })) as { role: string; text: string }[]; }
  /** 開いている人の操作の面の今の状態。 */
  async pending(): Promise<PendingView> { return (await this.#send({ op: "pending" })) as PendingView; }
  /** 主プロセスの出来事(前回の取り出しの後の分)。 */
  async events(): Promise<TestEvent[]> { return (await this.#send({ op: "events" })) as TestEvent[]; }
  close(): void { this.#sock.end(); }
}
