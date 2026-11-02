// 試験用ドライバのクライアント(完了条件 12)。試験用のアプリ(SITATE_TEST_DRIVER_SOCKET の UNIX ソケットを開く)に接続し、5 面を操作する。
// 受入試験の A2・A6・A7 の画面の操作はここから駆動する。製品のアプリには含めない。
import { connect } from "node:net";
import type { Socket } from "node:net";
import type { DriverCommand } from "../../src/testing/driver-server.ts";

export const SURFACES = ["chat", "approval-card", "secret-box", "phrase-panel", "status"] as const;
export type SurfaceName = (typeof SURFACES)[number];
type Reply = { result?: unknown; error?: string };

export class Driver {
  readonly #sock: Socket;
  #buf = "";
  readonly #waiters: ((r: Reply) => void)[] = [];

  constructor(sock: Socket) {
    this.#sock = sock;
    sock.on("data", (d) => {
      this.#buf += d.toString("utf8");
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
  close(): void { this.#sock.end(); }
}
