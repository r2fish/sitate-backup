// チャット欄の行を画面へ送る形にする(spec 3.7「画面の見た目」)。行の数と順と文は変えない。
// 続く道具の行(道具の呼び出しの行と手順の合格の行)の並びの最初の行に、件数の見出しの文(カタログ)を付ける。畳むかどうかは画面が決める。
import type { Translate } from "./approval-card.ts";
import type { UiMessage } from "./ipc-channels.ts";
import type { ChatLine } from "./session.ts";

export type UiChatLine = {
  readonly role: ChatLine["role"];
  readonly text: string;
  readonly spans?: ChatLine["spans"];
  /** 並びの最初の行だけ。見出しの文(並びの行が 2 つ以上のとき)。 */
  readonly fold?: string;
};

export function uiChatLines(lines: readonly ChatLine[], heading: (count: number) => string): UiChatLine[] {
  const out: UiChatLine[] = [];
  let i = 0;
  while (i < lines.length) {
    const l = lines[i]!;
    if (l.role !== "tool") { out.push({ role: l.role, text: l.text, ...(l.spans === undefined ? {} : { spans: l.spans }) }); i++; continue; }
    let j = i;
    while (j < lines.length && lines[j]!.role === "tool") j++;
    const run = lines.slice(i, j);
    const calls = run.filter((x) => x.step !== true).length;
    run.forEach((x, k) => out.push({ role: "tool", text: x.text, ...(k === 0 && run.length >= 2 ? { fold: heading(calls > 0 ? calls : run.length) } : {}) }));
    i = j;
  }
  return out;
}

/** チャット欄の描画の入力。応答の待ちの文は、待っている入力が 1 つ以上のときだけ送る。 */
export function chatMessage(chat: readonly ChatLine[], t: Translate, start: { guide: string; label: string } | null, waiting: number): UiMessage {
  return { surface: "chat", lines: uiChatLines(chat, (count) => t("chat.tool.group.heading", { count })), sendLabel: t("chat.send.label"), start, wait: waiting >= 1 ? t("chat.wait.label") : null };
}
