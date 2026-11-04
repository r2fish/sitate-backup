// 画面写しの口(作業指示書 5.14c の 7)。試験用ドライバの受け口(driver-server.ts)だけが呼ぶ。試験ビルドだけに入り、製品のビルドには入らない。
// capture: 窓の中身を PNG にして test/records/U3c/ の下へ書く(置き場の外へは書かない)。theme: 明暗を切り替える。mask: フレーズのパネルの語の欄を隠す。
// require=wait は、応答の待ちの印が出ていて、開いている面(ダイアログ)が無いときだけ撮る。
// 守り: フレーズのパネルが開いていて語の欄が隠れていないときは、capture は何も書かずに失敗する(語が写った PNG を作らない)。
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, isAbsolute, normalize } from "node:path";

export type ShotCommand =
  | { readonly op: "capture"; readonly path: string; readonly theme?: "light" | "dark"; readonly require?: "wait" }
  | { readonly op: "theme"; readonly theme: "light" | "dark" | "system" }
  | { readonly op: "mask" };

/** 画面と窓の差し込み口(実体は executeJavaScript、nativeTheme、capturePage)。 */
export interface ShotControl {
  eval(script: string): Promise<unknown>;
  setTheme(t: "light" | "dark" | "system"): void;
  png(): Promise<Uint8Array>;
}

const ALLOWED = /\/test\/records\/U3c\/[A-Za-z0-9._-]+\.png$/;

/** 書いてよいパス(絶対パスで、`test/records/U3c/` の直下の .png)。違えば例外。 */
export function checkShotPath(p: string): string {
  if (!isAbsolute(p) || normalize(p) !== p || !ALLOWED.test(p)) throw new Error("画面写しの置き場は test/records/U3c/ の直下の .png だけ");
  return p;
}

export const MASK_SCRIPT = `(()=>{const w=document.getElementById("phrase-words");if(!w)return false;w.style.visibility="hidden";return true})()`;
const GUARD_SCRIPT = `(()=>{const p=document.getElementById("phrase-panel"),w=document.getElementById("phrase-words");return !(p&&p.open&&w&&w.style.visibility!=="hidden")})()`;
const WAIT_SCRIPT = `(()=>{const w=document.getElementById("chat-wait");return !!w&&!w.hidden&&!document.querySelector("dialog[open]")})()`;
const sleep = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));
const REPAINT_MS = 300;

export function isShotCommand(c: { op: string }): c is ShotCommand { return c.op === "capture" || c.op === "theme" || c.op === "mask"; }

/** 戻り値: capture は書いたら true、require を満たさず書かなかったら false。 */
export async function handleShot(c: ShotCommand, ctl: ShotControl): Promise<boolean> {
  if (c.op === "theme") { ctl.setTheme(c.theme); await sleep(REPAINT_MS); return true; }
  if (c.op === "mask") return (await ctl.eval(MASK_SCRIPT)) === true;
  const path = checkShotPath(c.path);
  if (c.theme !== undefined) { ctl.setTheme(c.theme); await sleep(REPAINT_MS); }
  if (c.require === "wait" && (await ctl.eval(WAIT_SCRIPT)) !== true) return false;
  if ((await ctl.eval(GUARD_SCRIPT)) !== true) throw new Error("フレーズのパネルの語の欄が隠れていない: 写さない");
  const png = await ctl.png();
  if ((await ctl.eval(GUARD_SCRIPT)) !== true) throw new Error("フレーズのパネルの語の欄が隠れていない: 写さない");
  if (c.require === "wait" && (await ctl.eval(WAIT_SCRIPT)) !== true) return false; // 撮っている間に面が開いた(待ちの印の画面ではなくなった)
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, png);
  return true;
}
