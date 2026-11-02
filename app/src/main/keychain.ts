// キーチェーンの経路(完了条件 4・14)。主プロセスだけが触れる。macOS の `security` を `-i`(標準入力から命令を読む)で呼び、
// 秘密の値を引数(ps に見える)へ載せない: 値は 16 進(-X)にして標準入力へ渡す。
// 命名(GB-114): サービス名 = 固定の接頭辞 + "." + repo_id、アカウント名 = 参照名。repo_id が決まる前の項目は接頭辞の下の "pre" + 種別
// (LLM API キー: pre.llm、bootstrap の admin_ref: pre.admin、新しいホストの初期資格情報: pre.newhost)。接頭辞で全項目を列挙できる。
import { spawn } from "node:child_process";

export const KEYCHAIN_SERVICE_PREFIX = "jp.sitate-backup.app";
export const SERVICE_PRE_LLM = `${KEYCHAIN_SERVICE_PREFIX}.pre.llm`;
export const SERVICE_PRE_ADMIN = `${KEYCHAIN_SERVICE_PREFIX}.pre.admin`;
export const SERVICE_PRE_NEWHOST = `${KEYCHAIN_SERVICE_PREFIX}.pre.newhost`;
export const serviceFor = (repoId: string): string => `${KEYCHAIN_SERVICE_PREFIX}.${repoId}`;

/** security を実行する関数(試験では差し替える)。stdin を渡し、標準出力と終了コードを返す。 */
export type SecurityExec = (args: readonly string[], stdin: string) => Promise<{ stdout: string; code: number }>;

export interface Keychain {
  set(service: string, account: string, value: string): Promise<void>;
  get(service: string, account: string): Promise<string | null>;
  delete(service: string, account: string): Promise<boolean>;
  /** 接頭辞で始まるサービス名の項目の (service, account) の一覧(値は返さない)。 */
  list(prefix?: string): Promise<{ service: string; account: string }[]>;
}

const hex = (s: string): string => Buffer.from(s, "utf8").toString("hex");
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

  async set(service: string, account: string, value: string): Promise<void> {
    const r = await this.#exec(["-i"], `add-generic-password -U -s ${quote(service)} -a ${quote(account)} -X ${hex(value)}\n`);
    if (r.code !== 0) throw new Error("KEYCHAIN_WRITE_FAILED");
  }

  async get(service: string, account: string): Promise<string | null> {
    const r = await this.#exec(["-i"], `find-generic-password -s ${quote(service)} -a ${quote(account)} -w\n`);
    if (r.code !== 0) return null;
    return r.stdout.replace(/\n$/, "");
  }

  async delete(service: string, account: string): Promise<boolean> {
    const r = await this.#exec(["-i"], `delete-generic-password -s ${quote(service)} -a ${quote(account)}\n`);
    return r.code === 0;
  }

  async list(prefix: string = KEYCHAIN_SERVICE_PREFIX): Promise<{ service: string; account: string }[]> {
    const r = await this.#exec(["dump-keychain"], "");
    const out: { service: string; account: string }[] = [];
    for (const block of r.stdout.split(/^keychain: /m)) {
      const svc = /"svce"<blob>="([^"]*)"/.exec(block)?.[1];
      const acct = /"acct"<blob>="([^"]*)"/.exec(block)?.[1];
      if (svc !== undefined && acct !== undefined && svc.startsWith(prefix)) out.push({ service: svc, account: acct });
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
