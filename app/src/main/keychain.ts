// キーチェーンの経路(完了条件 4・14)。主プロセスだけが触れる。macOS の `security` を `-i`(標準入力から命令を読む)で呼び、
// 秘密の値を引数(ps に見える)へ載せない: 値は標準入力へ渡す。
// 値は全体を base64 にして保存する(複数行の SSH 秘密鍵を `-w` が 16 進で返すのを避ける)。`security -i` は 1 行が約 4 KB を超えると命令を読めないので、
// 長い値は CHUNK_CHARS 文字ごとに分け、先頭を `b64:<断片の数>:` を付けて本来のアカウント名に、残りを `<アカウント名>#c<番号>` に置く。
// 命名(GB-114。D-0159): サービス名は固定の 1 つ(v1 は 1 台のバックアップサーバー。別のサーバーの区別はイテレーション 2 以降)、
// アカウント名は参照名。アプリ自身の項目(LLM API キー、承認鍵、管理用 SSH 鍵)のアカウント名は参照名の形(英数字で始まり . _ - だけ)に
// 当たらない文字(":")を含め、参照名の項目とぶつからない。サービス名で、このアプリの項目を全部列挙できる(A2・A3 の「残っていない」の判定)。
import { spawn } from "node:child_process";

export const KEYCHAIN_SERVICE = "jp.sitate-backup.app";
/** 列挙の既定の接頭辞(固定のサービス名と同じ)。 */
export const KEYCHAIN_SERVICE_PREFIX = KEYCHAIN_SERVICE;
export const SERVICE_PRE_LLM = KEYCHAIN_SERVICE;
export const SERVICE_PRE_ADMIN = KEYCHAIN_SERVICE;
export const SERVICE_PRE_NEWHOST = KEYCHAIN_SERVICE;
/** アプリ自身の項目のアカウント名(承認鍵、管理用 SSH 鍵)。 */
export const APPROVAL_ACCOUNT = "app:approval-key";
export const ADMIN_KEY_ACCOUNT = "app:admin-ssh-key";
/** 項目のサービス名。引数(repo_id)は無視する: v1 は 1 台のバックアップサーバーで、サービス名は固定。 */
export const serviceFor = (_repoId?: string): string => KEYCHAIN_SERVICE;

/** security を実行する関数(試験では差し替える)。stdin を渡し、標準出力と終了コードを返す。 */
export type SecurityExec = (args: readonly string[], stdin: string) => Promise<{ stdout: string; code: number }>;

export interface Keychain {
  set(service: string, account: string, value: string): Promise<void>;
  get(service: string, account: string): Promise<string | null>;
  delete(service: string, account: string): Promise<boolean>;
  /** 接頭辞で始まるサービス名の項目の (service, account) の一覧(値は返さない)。 */
  list(prefix?: string): Promise<{ service: string; account: string }[]>;
}

/** 1 つの命令に載せる base64 の文字数(実測: 1 行 3,000 文字は通り 4,000 文字は通らない)。 */
const CHUNK_CHARS = 2000;
/** 断片の数の上限(1 項目 約 96 KB)。これを超える値は保存しない。 */
const MAX_CHUNKS = 64;
const HEAD = /^b64:(\d+):/;
const CHUNK_ACCOUNT = /#c\d+$/;
const chunkAccount = (account: string, i: number): string => (i === 0 ? account : `${account}#c${i}`);
/** `security -i` の 1 行へ載せる語。制御文字(改行を含む)を持つ語は命令の注入になり得るので、載せずに例外にする(最後の砦)。 */
const quote = (s: string): string => {
  if (/[\u0000-\u001f\u007f-\u009f\u2028\u2029]/.test(s)) throw new Error("KEYCHAIN_ARG_INVALID");
  return `"${s.replace(/(["\\])/g, "\\$1")}"`;
};

export const spawnSecurity: SecurityExec = (args, stdin) => new Promise((resolve, reject) => {
  const p = spawn("/usr/bin/security", [...args], { stdio: ["pipe", "pipe", "ignore"], env: {} });
  let out = "";
  p.stdout.on("data", (d: Buffer) => { out += d.toString("utf8"); });
  p.on("error", reject);
  p.on("close", (code) => resolve({ stdout: out, code: code ?? 1 }));
  p.stdin.end(stdin);
});

export class MacKeychain implements Keychain {
  readonly #exec: SecurityExec;
  constructor(exec: SecurityExec = spawnSecurity) {
    this.#exec = exec;
  }

  async #put(service: string, account: string, text: string): Promise<void> {
    const r = await this.#exec(["-i"], `add-generic-password -U -s ${quote(service)} -a ${quote(account)} -w ${quote(text)}\n`);
    if (r.code !== 0) throw new Error("KEYCHAIN_WRITE_FAILED");
  }

  async #read(service: string, account: string): Promise<string | null> {
    const r = await this.#exec(["-i"], `find-generic-password -s ${quote(service)} -a ${quote(account)} -w\n`);
    return r.code === 0 ? r.stdout.replace(/\n$/, "") : null;
  }

  async #remove(service: string, account: string): Promise<boolean> {
    return (await this.#exec(["-i"], `delete-generic-password -s ${quote(service)} -a ${quote(account)}\n`)).code === 0;
  }

  /** 断片(`#c1` から)を、無くなるまで消す。 */
  async #removeChunks(service: string, account: string): Promise<void> {
    for (let i = 1; i < MAX_CHUNKS && (await this.#remove(service, chunkAccount(account, i))); i++);
  }

  async set(service: string, account: string, value: string): Promise<void> {
    const b64 = Buffer.from(value, "utf8").toString("base64");
    const chunks: string[] = [];
    for (let at = 0; at < b64.length || chunks.length === 0; at += CHUNK_CHARS) chunks.push(b64.slice(at, at + CHUNK_CHARS));
    if (chunks.length > MAX_CHUNKS) throw new Error("KEYCHAIN_VALUE_TOO_LARGE");
    await this.#removeChunks(service, account);
    await this.#put(service, account, `b64:${chunks.length}:${chunks[0]}`);
    for (let i = 1; i < chunks.length; i++) await this.#put(service, chunkAccount(account, i), chunks[i]!);
  }

  async get(service: string, account: string): Promise<string | null> {
    const head = await this.#read(service, account);
    if (head === null) return null;
    const m = HEAD.exec(head);
    if (m === null) return head; // 旧い形(本アプリ以前の平文の項目)はそのまま返す
    let b64 = head.slice(m[0].length);
    for (let i = 1; i < Math.min(Number(m[1]), MAX_CHUNKS); i++) {
      const part = await this.#read(service, chunkAccount(account, i));
      if (part === null) return null;
      b64 += part;
    }
    return Buffer.from(b64, "base64").toString("utf8");
  }

  async delete(service: string, account: string): Promise<boolean> {
    const ok = await this.#remove(service, account);
    await this.#removeChunks(service, account);
    return ok;
  }

  async list(prefix: string = KEYCHAIN_SERVICE_PREFIX): Promise<{ service: string; account: string }[]> {
    const r = await this.#exec(["dump-keychain"], "");
    const out: { service: string; account: string }[] = [];
    for (const block of r.stdout.split(/^keychain: /m)) {
      const svc = /"svce"<blob>="([^"]*)"/.exec(block)?.[1];
      const acct = /"acct"<blob>="([^"]*)"/.exec(block)?.[1];
      if (svc !== undefined && acct !== undefined && svc.startsWith(prefix) && !CHUNK_ACCOUNT.test(acct)) out.push({ service: svc, account: acct });
    }
    return out;
  }
}

/** 試験用のメモリ上の実装。 */
export class MemoryKeychain implements Keychain {
  readonly items = new Map<string, string>();
  async set(s: string, a: string, v: string): Promise<void> { this.items.set(`${s}\u0000${a}`, v); }
  async get(s: string, a: string): Promise<string | null> { return this.items.get(`${s}\u0000${a}`) ?? null; }
  async delete(s: string, a: string): Promise<boolean> { return this.items.delete(`${s}\u0000${a}`); }
  async list(prefix: string = KEYCHAIN_SERVICE_PREFIX): Promise<{ service: string; account: string }[]> {
    return [...this.items.keys()].map((k) => k.split("\u0000") as [string, string]).filter(([s]) => s.startsWith(prefix)).map(([service, account]) => ({ service, account }));
  }
}
