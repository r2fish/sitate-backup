// チャット欄の行の描画(spec 3.7「画面の見た目」)。主プロセスが送った行 1 つを #chat-lines の直下の li 1 つで描く(役割 data-role と送った文そのもの data-text)。
// 続く道具の行の並びは、最初の行の li の中の見出しで畳む。最後の並びは開いたまま、人が開いた並びは描き直しても開いたまま。textContent と createElement だけ。
import { renderMarkdown } from "./markdown.ts";
import type { MdDoc, MdNode } from "./markdown.ts";

export interface ChatLineView { readonly role: "human" | "agent" | "tool" | "notice"; readonly text: string; readonly spans?: readonly (readonly [number, number])[]; readonly fold?: string }

/** 人が開閉した並び(並びの最初の行の番号 → 開いているか)。描き直しをまたいで持つ。 */
export type FoldState = Map<number, boolean>;

export function renderChatLines(doc: Document, list: HTMLElement, lines: readonly ChatLineView[], fold: FoldState): void {
  const items: HTMLElement[] = [];
  /** 並びの最初の行の番号 → 並びの li(最初の行を含む)。 */
  const runs = new Map<number, HTMLElement[]>();
  let run: HTMLElement[] | null = null;
  lines.forEach((l, i) => {
    const li = doc.createElement("li");
    li.setAttribute("data-role", l.role);
    li.setAttribute("data-text", l.text);
    const body = doc.createElement("div");
    body.setAttribute("class", "line-body");
    if (l.role === "agent") body.append(...(renderMarkdown(doc as unknown as MdDoc, l.text, l.spans ?? []) as unknown as Node[]));
    else body.textContent = l.text;
    li.append(body);
    items.push(li);
    if (l.role !== "tool") { run = null; return; }
    if (run === null) { run = []; runs.set(i, run); }
    run.push(li);
  });
  const lastRunStart = Math.max(-1, ...[...runs.keys()]);
  // 最後の並び: 後にエージェントの文がまだ来ていない並び(後に人の発話・知らせが来ていてもよい)。
  const lastIsTail = lastRunStart >= 0 && !lines.slice(lastRunStart).some((l) => l.role === "agent");
  for (const [start, lis] of runs) {
    const head = lines[start]!.fold;
    if (head === undefined || lis.length < 2) continue;
    const isOpen = (): boolean => fold.get(start) ?? (lastIsTail && start === lastRunStart);
    const btn = doc.createElement("button");
    btn.setAttribute("type", "button");
    btn.setAttribute("class", "fold-head");
    btn.setAttribute("data-fold", String(start));
    const arrow = doc.createElement("span");
    arrow.setAttribute("aria-hidden", "true");
    const label = doc.createElement("span");
    label.textContent = head;
    btn.append(label, arrow);
    const apply = (): void => {
      const open = isOpen();
      btn.setAttribute("aria-expanded", String(open));
      arrow.textContent = open ? " ▾" : " ▸";
      lis.slice(1).forEach((x) => { x.hidden = !open; });
    };
    btn.addEventListener("click", () => { fold.set(start, !isOpen()); apply(); });
    lis[0]!.prepend(btn);
    apply();
  }
  list.replaceChildren(...items);
}

export type { MdNode };
