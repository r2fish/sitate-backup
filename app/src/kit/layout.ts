// 欄を面ごとに上から積んで、配置の構造(mm)を決める。1 枚(表と裏)に収まらなければ undefined を返す。
import type { BlockSpec, Line } from "./model.ts";
import type { KitBlock, KitPage } from "./types.ts";
import { GAP_MM, GRID_COLS, GRID_ROW_MM, MARGIN_MM, PAD_MM, PAPER_H_MM } from "./metrics.ts";
import { CONTENT_W_MM, rowsMm, wrapBlock } from "./measure.ts";
import type { Row } from "./wrap.ts";

export interface Placed {
  readonly spec: BlockSpec;
  readonly block: KitBlock;
  /** 欄の各文を折った行(HTML はこれをそのまま刷る)。 */
  readonly rows: readonly (readonly Row[])[];
}

export const CONT_NOTE = "行末の » は改行ではなく、語の途中で行を折った印。次の行と間を空けずに続けて打つ。";

/** 折った行と、語の途中で折ったときの注記を足した文。 */
function fold(s: BlockSpec): { lines: Line[]; rows: Row[][] } {
  const lines = [...s.lines];
  let rows = wrapBlock(s.heading, lines, CONTENT_W_MM);
  if (rows.some((r) => r.some((x) => x.cont))) {
    lines.push({ t: CONT_NOTE });
    rows = wrapBlock(s.heading, lines, CONTENT_W_MM);
  }
  return { lines, rows };
}

/** 1 面分を積む。下の余白を越えたら undefined。 */
function stack(specs: readonly BlockSpec[], page: KitPage): Placed[] | undefined {
  const out: Placed[] = [];
  let y = MARGIN_MM;
  for (const spec of specs.filter((s) => s.page === page)) {
    const { lines, rows } = fold(spec);
    const grid = spec.grid === undefined ? 0 : Math.ceil(spec.grid.length / GRID_COLS) * GRID_ROW_MM;
    const h = Math.round((2 * PAD_MM + rowsMm(lines, rows) + grid) * 100) / 100;
    if (y + h > PAPER_H_MM - MARGIN_MM) return undefined;
    const box = { x_mm: MARGIN_MM, y_mm: Math.round(y * 100) / 100, w_mm: CONTENT_W_MM, h_mm: h };
    const block: KitBlock = spec.step_id === undefined
      ? { kind: spec.kind, page, order: out.length, box }
      : { kind: spec.kind, page, order: out.length, box, step_id: spec.step_id };
    out.push({ spec: { ...spec, lines }, block, rows });
    y += h + GAP_MM;
  }
  return out;
}

export function placeAll(specs: readonly BlockSpec[]): Placed[] | undefined {
  const front = stack(specs, "front");
  const back = stack(specs, "back");
  return front === undefined || back === undefined ? undefined : [...front, ...back];
}
