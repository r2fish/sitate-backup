// mp-app C2: 起動の煙試験。試験用の署名で組んだアプリ(make build-app-test の成果物 app/dist)を Electron の実物で起動し、
// 主プロセスが同梱物の署名を検証して窓を開き、画面が描かれ、LLM API キーの入力ボックスが開くところまで確かめる。
// 実物の LLM には繋がない(キーを入れない)。使い捨ての keychain とユーザーデータの置き場を使い、利用者の keychain と設定に触れない。
// 事前: make build-test && make sign-test && make build-app-test と、Electron の実行体(node app/node_modules/electron/install.js)。
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { Driver } from "../e2e/driver.ts";

const appRoot = join(import.meta.dirname, "..", "..");
const electron = join(appRoot, "node_modules", "electron", "dist", "Electron.app", "Contents", "MacOS", "Electron");
const sleep = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));

async function until<T>(f: () => Promise<T | null | false>, ms: number, what: () => string): Promise<T> {
  const end = Date.now() + ms;
  for (;;) {
    try { const v = await f(); if (v) return v; } catch { /* まだ */ }
    if (Date.now() > end) throw new Error("待ち切れない: " + what());
    await sleep(200);
  }
}

test("[mp-app C2] 起動の煙: 署名を検証して窓を開き、状態の面と LLM API キーの入力ボックスが描かれる", { timeout: 120000 }, async () => {
  assert.ok(existsSync(join(appRoot, "dist", "main.js")), "app/dist が無い。先に make build-app-test");
  assert.ok(existsSync(electron), "Electron の実行体が無い。node app/node_modules/electron/install.js");
  const dir = realpathSync(mkdtempSync(join(tmpdir(), "mpapp-smoke-")));
  const kc = join(dir, "t.keychain-db");
  const before = spawnSync("/usr/bin/security", ["list-keychains", "-d", "user"]).stdout.toString();
  for (const a of [["create-keychain", "-p", "tmp", kc], ["set-keychain-settings", kc], ["unlock-keychain", "-p", "tmp", kc]]) assert.equal(spawnSync("/usr/bin/security", a).status, 0, a[0]);
  const sock = join(dir, "d.sock");
  const p = spawn(electron, [appRoot, `--user-data-dir=${join(dir, "ud")}`], { env: { PATH: process.env.PATH ?? "", HOME: process.env.HOME ?? "", SITATE_TEST_DRIVER_SOCKET: sock, SITATE_TEST_KEYCHAIN: kc }, stdio: ["ignore", "pipe", "pipe"] });
  let log = "";
  p.stdout.on("data", (d: Buffer) => { log += d.toString(); });
  p.stderr.on("data", (d: Buffer) => { log += d.toString(); });
  let driver: Driver | null = null;
  try {
    driver = await until(async () => (existsSync(sock) ? await Driver.connect(sock) : null), 60000, () => "ドライバの受け口(署名の検証に通って窓が開く)\n" + log.slice(-2500));
    const status = await until(async () => { const t = await driver!.read("status"); return t !== null && t.trim() !== "" ? t : null; }, 30000, () => "状態の面の描画\n" + log.slice(-1500));
    assert.ok(status.length > 0);
    const box = await until(async () => { const t = await driver!.read("secret-box"); return t !== null && t.trim() !== "" ? t : null; }, 30000, () => "LLM API キーの入力ボックス\n" + log.slice(-1500));
    assert.match(box, /LLM/, "初回の起動で LLM API キーの入力ボックスが開く(描画の完了を待ってから送る。B8)");
    // 複数行の値を入力欄に入れられる(秘密鍵の貼り付け。C3)。
    assert.equal(await driver.fill("secret-box", "none", "line1\nline2"), true);
    assert.equal(p.exitCode, null, "主プロセスが落ちていない\n" + log.slice(-2000));
  } finally {
    driver?.close();
    p.kill("SIGTERM");
    await sleep(500);
    if (p.exitCode === null) p.kill("SIGKILL");
    spawnSync("/usr/bin/security", ["delete-keychain", kc]);
    assert.equal(spawnSync("/usr/bin/security", ["list-keychains", "-d", "user"]).stdout.toString(), before, "keychain の検索の一覧が元のまま");
    rmSync(dir, { recursive: true, force: true });
  }
});
