// bootstrap が相手のホストで実行するコマンドの組み立てと実行(spec 3.7 の `bootstrap` の段)。管理アクセスは強制コマンドの無い通常の SSH なので、
// ここだけがシェルの構文を使う。値は必ず単引用符で包み、利用者の入力(ホスト名・ユーザー名)をコマンドの文字列へ直接つながない。
import type { SshConnection } from "../ssh/connection.ts";

export const shq = (s: string): string => `'${s.replace(/'/g, `'\\''`)}'`;

export const SITATE_USER = "sitate";
export const SITATE_BIN = "/usr/local/bin/sitate";
export const STATE_DIR = "/var/lib/sitate";
export const LIBEXEC = "/usr/libexec/sitate";
export const SSHD_DROPIN = "/etc/ssh/sshd_config.d/00-sitate.conf";
export const SSHD_DROPIN_BODY = "PasswordAuthentication no\nPermitRootLogin prohibit-password\n";

export interface RemoteResult { readonly code: number; readonly out: string }

export class Remote {
  readonly #conn: SshConnection;
  /** root でなければ `sudo -n `。 */
  sudo = "";
  constructor(conn: SshConnection) {
    this.#conn = conn;
  }

  async run(command: string, stdin?: string | Buffer): Promise<RemoteResult> {
    const r = await this.#conn.exec(command, stdin === undefined ? {} : { stdin });
    if (r.disconnected) throw new RemoteDisconnected();
    return { code: r.code ?? 1, out: r.stdout.toString("utf8") };
  }

  /** root の権限で実行する(sudo の有無は determineRoot で決めた接頭辞)。 */
  root(command: string, stdin?: string | Buffer): Promise<RemoteResult> {
    return this.run(`${this.sudo}sh -c ${shq(command)}`, stdin);
  }

  /** `sitate` ユーザーとして核を呼ぶ(sudo が無い root は runuser に読み替える。spec 4.7)。 */
  async asSitate(args: string): Promise<RemoteResult> {
    const hasSudo = (await this.run("command -v sudo >/dev/null 2>&1")).code === 0;
    const prefix = hasSudo ? `${this.sudo || "sudo -n "}-u ${SITATE_USER}` : `runuser -u ${SITATE_USER} --`;
    return this.run(`${prefix} ${SITATE_BIN} ${args}`);
  }

  /** ファイルを置く(標準入力を tee で書き、所有者とモードを付ける)。親ディレクトリは作る。 */
  async put(path: string, bytes: Buffer, mode: string, owner = "root:root"): Promise<boolean> {
    const dir = path.slice(0, path.lastIndexOf("/"));
    const cmd = `install -d -m 0755 ${shq(dir)} && cat > ${shq(path)} && chmod ${mode} ${shq(path)} && chown ${owner} ${shq(path)}`;
    return (await this.root(cmd, bytes)).code === 0;
  }

  async sha256(path: string): Promise<string | null> {
    const r = await this.root(`sha256sum ${shq(path)}`);
    return r.code === 0 ? (r.out.trim().split(/\s+/)[0] ?? null) : null;
  }
}

export class RemoteDisconnected extends Error {}
