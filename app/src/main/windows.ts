// 窓の生成(完了条件 28: 面の列挙)。開く窓はアプリ本体の 1 つだけ(5 面はその中の区画とダイアログ)。ほかに開くのは OS の標準の窓(フォルダの選択、印刷)だけ。
// ナビゲーションと新しい窓の作成を拒み、レンダラの権限を絞る。
import { BrowserWindow, WebContentsView } from "electron";
import { join } from "node:path";

export const OS_STANDARD_DIALOGS = ["showOpenDialog", "print"] as const;

export function createMainWindow(resourcesDir: string): BrowserWindow {
  const win = new BrowserWindow({
    width: 1100, height: 760, show: false,
    webPreferences: { preload: join(resourcesDir, "preload.js"), contextIsolation: true, sandbox: true, nodeIntegration: false, webSecurity: true },
  });
  win.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  win.webContents.on("will-navigate", (e) => e.preventDefault());
  void win.loadFile(join(resourcesDir, "index.html"));
  win.once("ready-to-show", () => win.show());
  return win;
}

/**
 * 印刷だけの描画面(画面には出さない)。HTML をメモリのまま data URL で読み、OS の印刷の窓へ渡す。ファイルを作らない。
 * 窓ではなく画面に付けない WebContentsView を使う(開く窓はアプリ本体の 1 つと OS の標準の窓だけ。完了条件 28)。
 */
export async function printHtml(html: string): Promise<void> {
  const view = new WebContentsView({ webPreferences: { contextIsolation: true, sandbox: true, nodeIntegration: false, javascript: false } });
  try {
    await view.webContents.loadURL(`data:text/html;charset=utf-8;base64,${Buffer.from(html, "utf8").toString("base64")}`);
    await new Promise<void>((resolve, reject) => {
      view.webContents.print({ silent: false, printBackground: true }, (ok, reason) => (ok || reason === "cancelled" ? resolve() : reject(new Error(reason))));
    });
  } finally {
    view.webContents.close();
  }
}
