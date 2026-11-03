// 画面のプロセス(5 面の描画)。主プロセスが描画済みのモデルを送り、ここは DOM に置くだけ。ボタンは固定文を持たず、主プロセスが文を送る。
// エージェントのプロセスへの経路は無い(preload の窓口だけ)。textContent だけで描き、HTML として解釈しない。
import type { CardDecision, PhraseAction, UiMessage } from "../main/ipc-channels.ts";

interface Api {
  sendChat(text: string): Promise<void>;
  startChat(): Promise<void>;
  decideCard(changeId: string, d: CardDecision): Promise<void>;
  submitSecret(ref: string, value: string): Promise<void>;
  phraseAction(a: PhraseAction): Promise<void>;
  statusAction(kind: string): Promise<void>;
  registerTarget(host: string, port: string, fingerprint: string): Promise<void>;
  setFingerprint(fp: string): Promise<void>;
  removeTarget(): Promise<void>;
  onRender(cb: (m: UiMessage) => void): void;
}
declare const sitate: Api;

const $ = (id: string): HTMLElement => document.getElementById(id)!;
const el = (tag: string, text: string, attrs: Record<string, string> = {}): HTMLElement => {
  const e = document.createElement(tag);
  e.textContent = text;
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
  return e;
};
const button = (text: string, id: string, on: () => void): HTMLElement => {
  const b = el("button", text, { "data-action": id });
  b.addEventListener("click", on);
  return b;
};

interface CardView { changeId: string; approveEnabled: boolean; updatePrompt: string | null; proofRecipients: string[]; proofLabels: Record<string, string>; sections: { no: number; title: string; rows: { label: string; value: string }[]; lines: string[] }[]; labels: { approve: string; cancel: string; close: string } }

function renderCard(card: CardView): void {
  const body = $("card-body");
  body.replaceChildren(...card.sections.flatMap((s) => [el("h3", s.title, { "data-section": String(s.no) }), ...s.rows.map((r) => el("p", `${r.label} ${r.value}`.trim())), ...s.lines.map((l) => el("p", l))]));
  if (card.updatePrompt !== null) body.append(el("p", card.updatePrompt, { "data-update-prompt": "1" }));
  const proofs = $("card-proofs");
  proofs.replaceChildren(...card.proofRecipients.map((r) => { const l = el("label", card.proofLabels[r] ?? ""); l.append(el("input", "", { "data-proof": r, autocomplete: "off" })); return l; }));
  const collect = (): Record<string, string> => Object.fromEntries([...proofs.querySelectorAll("input")].map((i) => [i.getAttribute("data-proof")!, i.value]));
  const dlg = $("approval-card") as HTMLDialogElement;
  // 決定は 1 回だけ主プロセスへ送り、その場でカードを閉じる。決定の前に閉じられた(Esc)ときは「閉じる」として送る(会話を止めない)。
  let decided = false;
  const decide = (d: CardDecision): void => { if (decided) return; decided = true; if (dlg.open) dlg.close(); void sitate.decideCard(card.changeId, d); };
  const actions = [button(card.labels.cancel, "cancel", () => decide({ kind: "cancel" })), button(card.labels.close, "close", () => decide({ kind: "close" }))];
  if (card.approveEnabled) actions.unshift(button(card.labels.approve, "approve", () => { const p = collect(); proofs.querySelectorAll("input").forEach((i) => { i.value = ""; }); decide({ kind: "approve", proofs: p }); }));
  $("card-actions").replaceChildren(...actions);
  dlg.addEventListener("close", () => decide({ kind: "close" }), { once: true });
  dlg.showModal();
}

interface TargetView { registered: boolean; host: string | null; port: number | null; fingerprintPinned: boolean; notice: string | null; labels: { heading: string; host: string; port: string; fingerprint: string; register: string; remove: string; pinned: string } }

const field = (label: string, name: string, value = ""): HTMLElement => {
  const l = el("label", label);
  const i = el("input", "", { "data-target-field": name, autocomplete: "off" }) as HTMLInputElement;
  i.value = value;
  l.append(i);
  return l;
};
const valueOf = (root: HTMLElement, name: string): string => (root.querySelector(`[data-target-field="${name}"]`) as HTMLInputElement).value;

function renderTarget(v: TargetView): void {
  const root = $("target-area");
  const parts: HTMLElement[] = [el("h2", v.labels.heading), ...(v.notice === null ? [] : [el("p", v.notice, { "data-target-notice": "1" })])];
  if (v.registered) {
    parts.push(el("p", `${v.host ?? ""}:${v.port ?? ""}${v.fingerprintPinned ? ` ${v.labels.pinned}` : ""}`));
    if (!v.fingerprintPinned) parts.push(field(v.labels.fingerprint, "fingerprint"), button(v.labels.register, "target-fingerprint", () => void sitate.setFingerprint(valueOf(root, "fingerprint"))));
    parts.push(button(v.labels.remove, "target-remove", () => void sitate.removeTarget()));
  } else {
    parts.push(field(v.labels.host, "host"), field(v.labels.port, "port"), field(v.labels.fingerprint, "fingerprint"), button(v.labels.register, "target-register", () => void sitate.registerTarget(valueOf(root, "host"), valueOf(root, "port"), valueOf(root, "fingerprint"))));
  }
  root.replaceChildren(...parts);
}

sitate.onRender((m) => {
  switch (m.surface) {
    case "chat":
      $("chat-send").textContent = m.sendLabel;
      $("chat-lines").replaceChildren(...m.lines.map((l) => el("li", l.text, { "data-role": l.role })));
      // 最初の一歩(D-0169): 固定の案内と「始める」。文は主プロセスが送る(ここに固定文を持たない)。
      $("chat-start").replaceChildren(...(m.start === null ? [] : [el("p", m.start.guide, { "data-start-guide": "1" }), button(m.start.label, "chat-start", () => void sitate.startChat())]));
      return;
    case "approval-card": renderCard(m.card as CardView); return;
    case "secret-box": {
      $("secret-label").textContent = m.label;
      $("secret-submit").textContent = m.submitLabel;
      const input = $("secret-input") as HTMLTextAreaElement;
      const secretDialog = $("secret-box") as HTMLDialogElement;
      let done = false;
      const finish = (v: string): void => { if (done) return; done = true; input.value = ""; if (secretDialog.open) secretDialog.close(); void sitate.submitSecret(m.ref, v); };
      $("secret-submit").onclick = () => finish(input.value);
      secretDialog.addEventListener("close", () => finish(""), { once: true }); // Esc などで閉じたら取り消し(空の提出)
      ($("secret-box") as HTMLDialogElement).showModal();
      return;
    }
    case "phrase-panel": {
      const dlg = $("phrase-panel") as HTMLDialogElement;
      $("phrase-words").replaceChildren(...(m.words ?? []).map((w) => el("li", w.word)));
      $("phrase-cautions").replaceChildren(...m.cautions.map((c) => el("p", c)));
      const acts = m.actions.map((a) => button(a.label, a.action, () => void sitate.phraseAction(a.action)));
      $("phrase-actions").replaceChildren(...(m.notice === null ? [] : [el("p", m.notice, { "data-phrase-notice": "1" })]), ...acts);
      if (!m.open) { if (dlg.open) dlg.close(); } else if (!dlg.open) dlg.showModal();
      return;
    }
    case "status":
      $("status-heading").textContent = m.heading;
      $("status-rows").replaceChildren(...m.rows.map((r) => el("li", `${r.label} ${r.value}`.trim())));
      $("status-ask-ai").replaceChildren(...m.askAi.map((a) => button(a.label, `ask-ai:${a.kind}`, () => void sitate.statusAction(a.kind))));
      renderTarget(m.target);
      $("survey-area").replaceChildren(...(m.survey === null ? [] : [el("h2", m.survey.heading), ...m.survey.rows.map((r) => el("p", `${r.name} / ${r.kind} / ${r.size} / ${r.v1}`, { "data-survey-row": "1" }))]));
  }
});

// Esc でパネルを閉じさせない。閉じるのは「やめる」の操作(主プロセスが語を潰して閉じる)か、済んだとき。
$("phrase-panel").addEventListener("cancel", (e) => { e.preventDefault(); void sitate.phraseAction("cancel"); });

$("chat-form").addEventListener("submit", (e) => {
  e.preventDefault();
  const input = $("chat-input") as HTMLInputElement;
  const text = input.value;
  input.value = "";
  if (text.trim() !== "") void sitate.sendChat(text);
});
