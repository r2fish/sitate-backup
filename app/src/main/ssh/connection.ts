// 管理用 SSH の 1 接続(spec 3.7 の方式)。Node の SSH ライブラリ ssh2 を主プロセスで使う。システムの ssh・~/.ssh/config・ssh-agent・known_hosts は使わず、
// エージェント転送とポート転送を使わず(このファイルは shell・forward・subsystem を開かず、exec だけを開く)、ホスト鍵は Ed25519 に限って固定値と照らす。
import ssh2 from "ssh2";
import { SSH_KEEPALIVE_INTERVAL_S, SSH_KEEPALIVE_MAX } from "../../constants.ts";
import { decideHostKey } from "./fingerprint.ts";
import { fingerprintOf } from "./fingerprint.ts";

const { Client } = ssh2;

export type SshFailure =
  | { readonly code: "SSH_UNREACHABLE"; readonly reason: "dns" | "refused" | "timeout" }
  | { readonly code: "SSH_AUTH_DENIED" }
  | { readonly code: "SSH_HOSTKEY_MISMATCH" }
  | { readonly code: "SSH_DISCONNECTED" };

export class SshError extends Error {
  readonly failure: SshFailure;
  constructor(failure: SshFailure) {
    super(failure.code);
    this.failure = failure;
  }
}

export interface ConnectOptions {
  readonly host: string;
  readonly port: number;
  readonly username: string;
  /** OpenSSH 形式の秘密鍵(パスフレーズなし)か、パスワード。どちらか一方。 */
  readonly auth: { readonly privateKey: string } | { readonly password: string };
  /** 固定済みの指紋(接続先ファイルの値)。 */
  readonly pinnedFp: string | null;
  /** 人が入れた復旧キットの指紋(固定値が無いときの照合)。 */
  readonly typedFp: string | null;
  /** 初回接続で見えた鍵をそのまま固定してよいか(最初の bootstrap と bootstrap-restore だけ)。 */
  readonly allowFirstUse: boolean;
  readonly connectTimeoutMs: number;
}

export interface ExecResult {
  readonly stdout: Buffer;
  /** 終了コード。接続が切れて得られなければ null。 */
  readonly code: number | null;
  readonly disconnected: boolean;
}

export interface ExecOptions {
  readonly stdin?: string | Buffer;
}

/** 接続したことと、この接続で固定した指紋(固定したときだけ)。 */
export interface Connected {
  readonly hostKeyFp: string;
  readonly pinnedNow: boolean;
}

export class SshConnection {
  readonly #client = new Client();
  #closed = false;
  #connected: Connected | null = null;

  async connect(o: ConnectOptions): Promise<Connected> {
    let mismatch = false;
    let seenFp = "";
    let pinnedNow = false;
    const verify = (key: Buffer): boolean => {
      const d = decideHostKey(key, o.pinnedFp, o.typedFp, o.allowFirstUse);
      if (!d.ok) { mismatch = true; return false; }
      seenFp = fingerprintOf(key);
      pinnedNow = d.pin;
      return true;
    };
    await new Promise<void>((resolve, reject) => {
      this.#client.once("ready", () => resolve());
      this.#client.once("error", (e: Error & { level?: string }) => reject(new SshError(mismatch ? { code: "SSH_HOSTKEY_MISMATCH" } : classify(e))));
      this.#client.once("close", () => { this.#closed = true; });
      this.#client.connect({
        host: o.host, port: o.port, username: o.username,
        ..."privateKey" in o.auth ? { privateKey: o.auth.privateKey } : { password: o.auth.password },
        // エージェントは使わない(agent を渡さない)。Ed25519 のホスト鍵だけを受ける。
        algorithms: { serverHostKey: ["ssh-ed25519"] },
        hostVerifier: verify,
        readyTimeout: o.connectTimeoutMs,
        keepaliveInterval: SSH_KEEPALIVE_INTERVAL_S * 1000,
        keepaliveCountMax: SSH_KEEPALIVE_MAX,
        tryKeyboard: false,
      });
    });
    this.#connected = { hostKeyFp: seenFp, pinnedNow };
    return this.#connected;
  }

  get connected(): Connected | null {
    return this.#connected;
  }

  /** コマンド 1 本を exec で実行する(強制コマンドの接続では SSH_ORIGINAL_COMMAND になる)。切断は結果の disconnected で返す。 */
  exec(command: string, opts: ExecOptions = {}): Promise<ExecResult> {
    return new Promise((resolve) => {
      if (this.#closed) return resolve({ stdout: Buffer.alloc(0), code: null, disconnected: true });
      this.#client.exec(command, (err, stream) => {
        if (err) return resolve({ stdout: Buffer.alloc(0), code: null, disconnected: true });
        const chunks: Buffer[] = [];
        let code: number | null = null;
        stream.on("data", (c: Buffer) => { chunks.push(c); });
        stream.stderr.on("data", () => undefined); // 標準エラーは診断用。読み捨てる(秘密と対象のファイル名を含めない約束だが、モデルにも記録にも渡さない)。
        stream.on("exit", (c: number | null) => { code = c; });
        let done = false;
        const finish = (): void => {
          if (done) return;
          done = true;
          resolve({ stdout: Buffer.concat(chunks), code, disconnected: code === null });
        };
        stream.on("close", finish);
        this.#client.once("close", finish);
        if (opts.stdin !== undefined) stream.end(opts.stdin); else stream.end();
      });
    });
  }

  close(): void {
    this.#client.end();
  }
}

/** ssh2 のエラーを spec 3.7 のアプリの原因コードの理由に分ける。 */
export function classify(e: Error & { code?: string; level?: string }): SshFailure {
  const m = e.message;
  if (e.level === "client-authentication" || /authentication methods failed/i.test(m)) return { code: "SSH_AUTH_DENIED" };
  if (/no matching host key|host key/i.test(m)) return { code: "SSH_HOSTKEY_MISMATCH" };
  if (e.code === "ENOTFOUND" || e.code === "EAI_AGAIN") return { code: "SSH_UNREACHABLE", reason: "dns" };
  if (e.code === "ECONNREFUSED") return { code: "SSH_UNREACHABLE", reason: "refused" };
  if (/timed out|timeout/i.test(m) || e.level === "client-timeout" || e.code === "ETIMEDOUT" || e.code === "EHOSTUNREACH" || e.code === "ENETUNREACH") return { code: "SSH_UNREACHABLE", reason: "timeout" };
  if (/ECONNRESET|handshake|socket|end/i.test(m)) return { code: "SSH_DISCONNECTED" };
  return { code: "SSH_UNREACHABLE", reason: "timeout" };
}
