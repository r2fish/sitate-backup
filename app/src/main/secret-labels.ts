// 秘密の入力ボックスの文(ラベル)と値の形を、道具と引数の名前からアプリが決める(spec 3.7。モデルが決めた文や参照名は画面に出さない)。
// 何の秘密かを人に正しく伝える: 同じ文で別の秘密を求めると、乗っ取られたエージェントが人に違う値を入れさせやすい。
import { SECRET_LABEL } from "./secret-box.ts";

/** キーチェーンの参照名を取る引数(値は `--secrets-stdin` の 1 行目に差し込む)。`key_ref` は保管先の鍵で、値はホストの秘密ファイルから読むので含めない。 */
export const KEYCHAIN_REF_ARGS = ["admin_ref", "source_admin_ref", "host_ref", "secret_ref"] as const;
/** 事業者の管理用鍵を取る道具(保存しないモード。spec 3.7 の写像)。 */
export const BUSINESS_KEY_TOOLS: ReadonlySet<string> = new Set(["sitate_init_storage", "sitate_init", "sitate_apply"]);

/** 入力ボックスのラベルのカタログのキー。 */
export function labelFor(tool: string, arg: string): string {
  if (arg === "secret_ref" && tool === "sitate_notify_set") return SECRET_LABEL.webhook;
  if (arg === "admin_ref" && BUSINESS_KEY_TOOLS.has(tool)) return SECRET_LABEL.storageAdmin;
  if (arg === "source_admin_ref" || (arg === "admin_ref" && tool.startsWith("sitate_source_"))) return SECRET_LABEL.sourceAdmin;
  if (arg === "host_ref" || (arg === "admin_ref" && tool.startsWith("sitate_restore_dest_"))) return SECRET_LABEL.destAdmin;
  if (arg === "admin_ref" && tool === "sitate_bootstrap") return SECRET_LABEL.admin;
  return SECRET_LABEL.generic;
}

/**
 * 画面で入れた文字列を、核が受ける値の形にする(spec 3.1.1 の秘密の入力の規則)。
 *  - 秘密鍵(PRIVATE KEY を含む)は原文のまま(改行を保つ)。
 *  - 保管先の事業者の管理用鍵は `{"key_id":"…","secret":"…"}`。JSON のオブジェクトで入れたらそのまま、2 行(1 行目がキー ID、2 行目が鍵)なら組み立てる。
 *  - ほかの文字列(パスワード、Webhook の URL)は前後の空白と改行を除く(Enter で送った改行が値に入らないように)。
 */
export function shapeSecret(tool: string, arg: string, text: string): unknown {
  if (text.includes("PRIVATE KEY")) return text;
  const t = text.trim();
  if (arg === "admin_ref" && BUSINESS_KEY_TOOLS.has(tool)) {
    try {
      const j: unknown = JSON.parse(t);
      if (typeof j === "object" && j !== null && !Array.isArray(j)) return j;
    } catch { /* 2 行の形を試す */ }
    const lines = t.split(/\r?\n/).map((l) => l.trim()).filter((l) => l !== "");
    if (lines.length === 2) return { key_id: lines[0], secret: lines[1] };
  }
  return t;
}
