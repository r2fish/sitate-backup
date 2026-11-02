// CoreRunner の実体(spec 3.7): 管理用 SSH で `sitate gate` の強制コマンドへ引数列を送り、標準出力を返す。接続は呼び出しごとに張り、固定値と照らす。
// 引数列は字句規則(spec 3.1.5 の規則 2・5・6)に従い、空白 1 つで連結する。規則に合わない語を含む引数列は送らずに失敗とする。
import { randomBytes } from "node:crypto";
import type { StreamEnd } from "../stdout-records.ts";
import { SshConnection, SshError } from "./connection.ts";
import type { ExecResult } from "./connection.ts";
import type { TargetStore } from "./target-store.ts";

export interface CoreRunnerDeps {
  readonly targets: TargetStore;
  /** キーチェーンの管理用 SSH 鍵(OpenSSH 形式)。無ければ null。 */
  readonly adminKey: () => Promise<string | null>;
  readonly connectTimeoutMs: number;
  /** 試験で接続の実装を差し替える。 */
  readonly newConnection?: () => SshConnection;
}

/** 字句規則 2: 空の語、空白・制御文字(0x00〜0x1F、0x7F)を含む語は送れない。 */
export function commandLine(argv: readonly string[]): string | null {
  if (argv.length === 0) return null;
  for (const w of argv) if (w === "" || /[\u0000- \u007f]/.test(w)) return null;
  return argv.join(" ");
}

export class UnsendableArgvError extends Error {}

/** `r-<UTC 開始時刻>-<乱数 16 ビット>`(spec 3.1.1)。 */
export function newRunId(now: Date = new Date()): string {
  const ts = now.toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
  return `r-${ts}-${randomBytes(2).toString("hex")}`;
}

export class SshCoreRunner {
  readonly #d: CoreRunnerDeps;
  constructor(deps: CoreRunnerDeps) {
    this.#d = deps;
  }

  async run(argv: readonly string[], stdin: string): Promise<{ stdout: string; end: StreamEnd }> {
    const r = await this.#exec(argv, stdin);
    return { stdout: r.stdout.toString("utf8"), end: r.disconnected ? "ssh_disconnected" : "complete" };
  }

  async #exec(argv: readonly string[], stdin: string): Promise<ExecResult> {
    const line = commandLine(argv);
    if (line === null) throw new UnsendableArgvError("引数列が字句規則に合わない");
    const t = this.#d.targets.read();
    const key = await this.#d.adminKey();
    if (t === null) throw new SshError({ code: "SSH_UNREACHABLE", reason: "dns" });
    if (key === null) throw new SshError({ code: "SSH_AUTH_DENIED" });
    const conn = (this.#d.newConnection ?? (() => new SshConnection()))();
    try {
      await conn.connect({ host: t.host, port: t.port, username: t.user, auth: { privateKey: key }, pinnedFp: t.hostKeyFp, typedFp: null, allowFirstUse: false, connectTimeoutMs: this.#d.connectTimeoutMs });
      return await conn.exec(line, { stdin });
    } finally {
      conn.close();
    }
  }
}
