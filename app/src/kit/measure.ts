// 欄の中身を折り返して行にし、高さを数える(画素でなく mm で配置を決める)。折り返しは wrap.ts の規則で、HTML もこの行をそのまま刷る。
import type { Line } from "./model.ts";
import { BODY_PT, HEAD_PT, LINE_RATIO, MARGIN_MM, MONO_PT, PAD_MM, PT_MM } from "./metrics.ts";
import { textEm, wrapLine, type Row } from "./wrap.ts";

export const CONTENT_W_MM = 210 - 2 * MARGIN_MM;

/** 太字の見出しの幅の余裕(太字は同じ字でも広い)。 */
const BOLD_FACTOR = 1.12;

export function linePt(l: Line): number {
  return l.mono ? MONO_PT : BODY_PT;
}

export function lineHeightMm(pt: number): number {
  return pt * PT_MM * LINE_RATIO;
}

/** 欄の各文を折った行。見出しは最初の行の頭に続けて刷る(行を使わない)。 */
export function wrapBlock(heading: string, lines: readonly Line[], boxW: number): Row[][] {
  const inner = boxW - 2 * PAD_MM;
  const headMm = (textEm(heading, false) + 0.3) * HEAD_PT * PT_MM * BOLD_FACTOR;
  return lines.map((l, i) => wrapLine(l, linePt(l), inner, i === 0 ? headMm : 0));
}

/** 折った行の高さ(最初の行は見出しの大きさを含む)。 */
export function rowsMm(lines: readonly Line[], rows: readonly (readonly Row[])[]): number {
  let h = 0;
  rows.forEach((r, i) => {
    const pt = linePt(lines[i]);
    h += lineHeightMm(i === 0 ? Math.max(pt, HEAD_PT) : pt) + (r.length - 1) * lineHeightMm(pt);
  });
  return h;
}
