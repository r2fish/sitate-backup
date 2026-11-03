// 試験用の Electron アプリの起動と後始末。使い捨ての keychain とユーザーデータの置き場を使い、利用者の keychain と設定に触れない。
// 作業端末の初期化(キーチェーンの項目の削除、アプリのデータの置き場の削除、固定したホスト鍵の削除。固定の指紋はデータの置き場の ssh-target.json にある)は、
// 状態の置き場を作り直すこと(`resetState`)で行う。状態の置き場を指定しない回は毎回使い捨てで、A2 の各回はこれで初期化から始まる。
import { spawn, spawnSync } from "node:child_process";
import type { ChildProcess } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Driver } from "./driver.ts";

const SECURITY = "/usr/bin/security";
const KC_PASSWORD = "tmp"; // 使い捨ての keychain の鍵(利用者の keychain ではない。秘密ではない)
export const appRoot = join(import.meta.dirname, "..", "..");

export interface State { readonly dir: string; readonly keychain: string; readonly userData: string; readonly temporary: boolean }

export function prepareState(stateDir: string | undefined, reset: boolean): State {
  const temporary = stateDir === undefined;
  const dir = temporary ? realpathSync(mkdtempSync(join(tmpdir(), "sitate-e2e-"))) : stateDir;
  mkdirSync(dir, { recursive: true });
  const st: State = { dir, keychain: join(dir, "t.keychain-db"), userData: join(dir, "ud"), temporary };
  if (reset) resetState(st);
  if (!existsSync(st.keychain)) {
    for (const a of [["create-keychain", "-p", KC_PASSWORD, st.keychain], ["set-keychain-settings", st.keychain]]) {
      if (spawnSync(SECURITY, a).status !== 0) throw new Error("使い捨ての keychain を作れない: " + a[0]);
    }
  }
  if (spawnSync(SECURITY, ["unlock-keychain", "-p", KC_PASSWORD, st.keychain]).status !== 0) throw new Error("使い捨ての keychain を開けない");
  return st;
}

/** 作業端末の初期化: キーチェーンの項目、アプリのデータの置き場(固定したホスト鍵を含む)を消す。 */
export function resetState(st: State): void {
  if (existsSync(st.keychain)) spawnSync(SECURITY, ["delete-keychain", st.keychain]);
  rmSync(st.keychain, { force: true });
  rmSync(st.userData, { recursive: true, force: true });
}

/** キーチェーンの項目の一覧(サービス名・アカウント名だけ。値は読まない。A2・A3 の「残っていない」の判定)。 */
export function keychainItems(st: State, prefix = "jp.sitate-backup.app"): { service: string; account: string }[] {
  const out = spawnSync(SECURITY, ["dump-keychain", st.keychain]).stdout.toString();
  const items: { service: string; account: string }[] = [];
  for (const block of out.split(/^keychain: /m)) {
    const svc = /"svce"<blob>="([^"]*)"/.exec(block)?.[1];
    const acct = /"acct"<blob>="([^"]*)"/.exec(block)?.[1];
    if (svc !== undefined && acct !== undefined && svc.startsWith(prefix)) items.push({ service: svc, account: acct });
  }
  return items;
}

export function disposeState(st: State): void {
  if (!st.temporary) return;
  spawnSync(SECURITY, ["delete-keychain", st.keychain]);
  rmSync(st.dir, { recursive: true, force: true });
}

export interface Launched { readonly proc: ChildProcess; readonly driver: Driver; readonly log: () => string; stop(): Promise<void> }

const sleep = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));

export async function launchApp(o: { state: State; llmBaseUrl: string; hostMap: string; lang: "ja" | "en" }): Promise<Launched> {
  const electron = join(appRoot, "node_modules", "electron", "dist", "Electron.app", "Contents", "MacOS", "Electron");
  if (!existsSync(join(appRoot, "dist", "main.js"))) throw new Error("app/dist が無い。先に make build-app-test");
  if (!existsSync(electron)) throw new Error("Electron の実行体が無い。node app/node_modules/electron/install.js");
  const sock = join(o.state.dir, "d.sock");
  rmSync(sock, { force: true });
  // 人の役の値(SITATE_ROLE_*)や ANTHROPIC_* を継がせない: 環境は最小にする。
  const env = { PATH: process.env.PATH ?? "", HOME: process.env.HOME ?? "", SITATE_TEST_DRIVER_SOCKET: sock, SITATE_TEST_KEYCHAIN: o.state.keychain, SITATE_TEST_LLM_BASE_URL: o.llmBaseUrl, SITATE_TEST_HOST_MAP: o.hostMap };
  const proc = spawn(electron, [appRoot, `--user-data-dir=${o.state.userData}`, `--lang=${o.lang}`], { env, stdio: ["ignore", "pipe", "pipe"] });
  let log = "";
  const keep = (d: Buffer): void => { log = (log + d.toString()).slice(-20000); };
  proc.stdout?.on("data", keep);
  proc.stderr?.on("data", keep);
  let driver: Driver | null = null;
  for (let i = 0; i < 300 && driver === null; i++) {
    if (proc.exitCode !== null) throw new Error("アプリが起動できなかった\n" + log.slice(-2000));
    if (existsSync(sock)) driver = await Driver.connect(sock).catch(() => null);
    if (driver === null) await sleep(200);
  }
  if (driver === null) throw new Error("ドライバの受け口が開かない(署名の検証に通って窓が開くはず)\n" + log.slice(-2000));
  const d = driver;
  return {
    proc, driver: d, log: () => log,
    async stop() { d.close(); proc.kill("SIGTERM"); await sleep(500); if (proc.exitCode === null) proc.kill("SIGKILL"); },
  };
}
