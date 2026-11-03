// 試験用ドライバの受け口(完了条件 12)。試験用のアプリのビルド(`make build-app-test`)だけが束ね、製品のビルドには入れない
// (scripts/build-app.mjs が成果物にこのモジュールが無いことを検査する)。UNIX ソケットで 1 行 1 命令の JSON を受け、画面の 5 面を操作する。
import { createServer } from "node:net";
import type { Server } from "node:net";

/** 画面の操作の差し込み口(実体は webContents.executeJavaScript)。 */
export interface PageControl {
  eval(script: string): Promise<unknown>;
}

export type DriverCommand =
  | { readonly op: "chat"; readonly text: string }
  | { readonly op: "click"; readonly surface: string; readonly action: string }
  | { readonly op: "fill"; readonly surface: string; readonly target: string; readonly value: string }
  | { readonly op: "read"; readonly surface: string }
  | { readonly op: "lang"; readonly lang: "ja" | "en" }
  /** チャット欄の行(役割つき)。 */
  | { readonly op: "lines" }
  /** 開いている人の操作の面(秘密の入力ボックス、承認カード、フレーズのパネル)の今の状態。 */
  | { readonly op: "pending" }
  /** 主プロセスの出来事(道具の呼び出し・実行の始まりと終わり)を、前回の取り出しの後の分だけ取り出す。 */
  | { readonly op: "events" };

/** 主プロセスの出来事(attach.ts が積む)。kind: tool_call | run_start | run_end。 */
export type TestEvent = { readonly at: string; readonly kind: string } & Record<string, unknown>;

const q = JSON.stringify;

const PENDING_SCRIPT = `(()=>{const o=(id)=>{const d=document.getElementById(id);return d!==null&&d.open};const t=(s)=>[...document.querySelectorAll(s)];
return {secret:o("secret-box")?document.getElementById("secret-label").textContent:null,
card:o("approval-card")?{proofs:t("#card-proofs [data-proof]").map((i)=>i.getAttribute("data-proof")),actions:t("#card-actions [data-action]").map((b)=>({action:b.getAttribute("data-action"),disabled:b.disabled})),text:document.getElementById("card-body").innerText}:null,
phrase:o("phrase-panel")?{words:t("#phrase-words li").map((l)=>l.textContent),actions:t("#phrase-actions [data-action]").map((b)=>b.getAttribute("data-action")),notice:(document.querySelector("[data-phrase-notice]")||{}).textContent||null}:null}})()`;

/** 命令を画面の JavaScript にする。surface は data-surface の値、action は data-action、target は data-proof などの欄の識別。 */
export function scriptFor(c: DriverCommand): string {
  switch (c.op) {
    case "chat": return `(()=>{const i=document.getElementById("chat-input");i.value=${q(c.text)};document.getElementById("chat-form").requestSubmit();return true})()`;
    case "click": return `(()=>{const b=document.querySelector('[data-surface=${q(c.surface)}] [data-action=${q(c.action)}]');if(!b||b.disabled)return false;b.click();return true})()`;
    case "fill": return `(()=>{const i=document.querySelector('[data-surface=${q(c.surface)}] [data-proof=${q(c.target)}], [data-surface=${q(c.surface)}] input, [data-surface=${q(c.surface)}] textarea');if(!i)return false;i.value=${q(c.value)};return true})()`;
    case "read": return `(()=>{const s=document.querySelector('[data-surface=${q(c.surface)}]');return s?s.innerText:null})()`;
    case "lang": case "events": return "true";
    case "lines": return `[...document.querySelectorAll('#chat-lines li')].map((li)=>({role:li.getAttribute('data-role'),text:li.textContent}))`;
    case "pending": return PENDING_SCRIPT;
  }
}

export function startDriverServer(socketPath: string, page: PageControl, setLang: (l: "ja" | "en") => void, drainEvents: () => TestEvent[] = () => []): Server {
  const server = createServer((sock) => {
    let buf = "";
    sock.on("data", (d) => {
      buf += d.toString("utf8");
      for (let nl = buf.indexOf("\n"); nl >= 0; nl = buf.indexOf("\n")) {
        const line = buf.slice(0, nl);
        buf = buf.slice(nl + 1);
        void handle(line).then((r) => sock.write(JSON.stringify({ result: r }) + "\n"), (e: unknown) => sock.write(JSON.stringify({ error: String(e) }) + "\n"));
      }
    });
  });
  async function handle(line: string): Promise<unknown> {
    const c = JSON.parse(line) as DriverCommand;
    if (c.op === "lang") { setLang(c.lang); return true; }
    if (c.op === "events") return drainEvents();
    return page.eval(scriptFor(c));
  }
  server.listen(socketPath);
  return server;
}
