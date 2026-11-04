// 状態と報告の表示の描画(spec 3.7「画面の見た目」)。主プロセスが送った欄(ラベルと値)ごとに要素を分けて描く。文は変えない。
export interface StepView { readonly n: number; readonly label: string; readonly state: "passed" | "next" | "todo" }
export interface SurveyRowView { readonly name: string; readonly kind: string; readonly size: string; readonly v1: string; readonly v1ok: boolean }

const span = (doc: Document, cls: string, text: string): HTMLElement => {
  const s = doc.createElement("span");
  s.setAttribute("class", cls);
  s.textContent = text;
  return s;
};

/** ラベルと値を別の要素で並べた行。値が空なら値の要素を置かない。 */
export function fieldRow(doc: Document, tag: string, label: string, value: string, attrs: Record<string, string> = {}): HTMLElement {
  const r = doc.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) r.setAttribute(k, v);
  r.append(span(doc, "f-label", label));
  if (value !== "") r.append(span(doc, "f-value", value));
  return r;
}

export function renderSteps(doc: Document, root: HTMLElement, steps: readonly StepView[] | null): void {
  root.hidden = steps === null;
  root.replaceChildren(...(steps ?? []).map((s) => {
    const li = doc.createElement("li");
    li.setAttribute("data-step", String(s.n));
    li.setAttribute("data-state", s.state);
    li.setAttribute("value", String(s.n));
    li.append(span(doc, "step-mark", s.state === "passed" ? "✓" : s.state === "next" ? "→" : ""), span(doc, "step-label", s.label));
    return li;
  }));
}

export function surveyRow(doc: Document, r: SurveyRowView): HTMLElement {
  const p = doc.createElement("p");
  p.setAttribute("data-survey-row", "1");
  p.append(span(doc, "s-name", r.name), span(doc, "s-kind", r.kind), span(doc, "s-size", r.size), span(doc, r.v1ok ? "tag tag-ok" : "tag tag-ng", r.v1));
  return p;
}
