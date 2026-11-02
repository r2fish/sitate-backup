// 文字幅の見積りと、語の境界での折り返し。見積りと描画は同じ折り返しの結果(行の配列)を使う。ブラウザの折り返しには任せない。
import type { Line } from "./model.ts";
import { HALF_EM, MARK_EM, PROP_EM, PROP_SPACE_EM, PROP_UPPER_EM, PROP_WIDE_EM, WIDTH_SAFETY } from "./metrics.ts";

/** 折り返した 1 行。cont が真なら語の途中で折った印(»)を行末に刷る。 */
export interface Row {
  readonly text: string;
  readonly cont: boolean;
}

export function isWide(ch: string): boolean {
  const c = ch.codePointAt(0) ?? 0;
  return c >= 0x2e80 || (c >= 0x2000 && c <= 0x2fff);
}

/** 字の幅(em)。等幅は半角を HALF_EM、比例は字ごとの余裕のある値、全角は 1。 */
export function charEm(ch: string, mono: boolean): number {
  if (isWide(ch)) return 1;
  if (mono) return HALF_EM;
  if (ch === " ") return PROP_SPACE_EM;
  if ("mwMW@%&".includes(ch)) return PROP_WIDE_EM;
  if (ch >= "A" && ch <= "Z") return PROP_UPPER_EM;
  return PROP_EM;
}

export function textEm(s: string, mono: boolean): number {
  let w = 0;
  for (const ch of s) w += charEm(ch, mono);
  return w;
}

const CLOSERS = "、。,.)」』）】!?！？:;：；";

interface Atom {
  readonly sep: string;
  readonly text: string;
}

/** 語に分ける。全角の字は 1 字ずつ(閉じの句読点は前の字に付ける)、半角は空白までを 1 語にする。 */
function atomsOf(line: Line): Atom[] {
  if (line.units !== undefined) return line.units.map((u, i) => ({ sep: i === 0 ? "" : " ", text: u }));
  const out: Atom[] = [];
  let sep = "";
  let cur = "";
  const flush = () => {
    if (cur === "") return;
    out.push({ sep, text: cur });
    cur = "";
    sep = "";
  };
  for (const ch of line.t) {
    if (ch === " ") {
      flush();
      sep += " ";
    } else if (isWide(ch)) {
      flush();
      const prev = out[out.length - 1];
      if (sep === "" && prev !== undefined && CLOSERS.includes(ch)) out[out.length - 1] = { sep: prev.sep, text: prev.text + ch };
      else out.push({ sep, text: ch });
      sep = "";
    } else cur += ch;
  }
  flush();
  return out;
}

/** 1 語が 1 行に収まらないときだけ、収まる長さで折る(行末に印を刷る分の幅を空ける)。 */
function chunk(text: string, cap: number, mono: boolean, firstFree: number): Row[] {
  const rows: Row[] = [];
  let cur = "";
  let used = 0;
  let free = firstFree;
  for (const ch of text) {
    const w = charEm(ch, mono);
    if (used + w > free - MARK_EM && cur !== "") {
      rows.push({ text: cur, cont: true });
      cur = "";
      used = 0;
      free = cap;
    }
    cur += ch;
    used += w;
  }
  rows.push({ text: cur, cont: false });
  return rows;
}

/** 幅 widthMm の欄で、先頭の行の頭を offsetMm 空けて折った行。 */
export function wrapLine(line: Line, pt: number, widthMm: number, offsetMm: number): Row[] {
  const mono = line.mono === true;
  const emMm = pt * 0.3528;
  const cap = (widthMm * WIDTH_SAFETY) / emMm;
  const rows: Row[] = [];
  let cur = "";
  let used = offsetMm / emMm;
  const byUnit = line.units !== undefined;
  let atoms = atomsOf(line);
  while (atoms.length > 0) {
    const [a, ...rest] = atoms;
    atoms = rest;
    const w = textEm(a.text, mono);
    const sw = cur === "" ? 0 : textEm(a.sep, mono);
    if (used + sw + w <= cap) {
      cur += (cur === "" ? "" : a.sep) + a.text;
      used += sw + w;
      continue;
    }
    if (cur !== "") {
      rows.push({ text: cur, cont: false });
      cur = "";
      used = 0;
      atoms = [a, ...atoms];
      continue;
    }
    // 空の行にも収まらない語
    const parts = a.text.split(" ");
    if (parts.length > 1 && byUnit) {
      atoms = [...parts.map((p, i) => ({ sep: i === 0 ? "" : " ", text: p })), ...atoms];
      continue;
    }
    const pieces = chunk(a.text, cap, mono, cap - used);
    const last = pieces.pop() as Row;
    rows.push(...pieces);
    cur = last.text;
    used = textEm(cur, mono);
  }
  rows.push({ text: cur, cont: false });
  return rows;
}
