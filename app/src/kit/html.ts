// 配置の構造どおりに欄を絶対位置で置いた HTML 1 文書を書き出す(外部の資源を読まない。フォントは埋め込み)。メモリの文字列を返すだけで、ファイルに書かない。
// 文は配置で折った行をそのまま 1 行ずつ刷る(ブラウザの折り返しに任せない)ので、見積りと実際の行数が一致する。
import type { Placed } from "./layout.ts";
import { fontCss } from "./font.ts";
import { BODY_PT, GRID_COLS, GRID_PT, GRID_ROW_MM, HEAD_PT, LINE_RATIO, MONO_PT, PAD_MM, PAPER_H_MM, PAPER_W_MM } from "./metrics.ts";

export function esc(s: string): string {
  return s.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
}

const MONO_FAMILY = 'ui-monospace,Menlo,"Courier New","SitateJP",monospace';
const CSP = "default-src 'none'; style-src 'unsafe-inline'; font-src data:";

const CSS = `@page{size:A4 portrait;margin:0}
*{box-sizing:border-box}
html,body{margin:0;padding:0}
body{font-family:"SitateJP","Hiragino Sans","Noto Sans CJK JP",sans-serif;color:#000;background:#fff;line-height:${LINE_RATIO}}
.page{position:relative;width:${PAPER_W_MM}mm;height:${PAPER_H_MM}mm;overflow:hidden;break-after:page;page-break-after:always}
.page:last-child{break-after:auto;page-break-after:auto}
.blk{position:absolute;border:0.2mm solid #000;padding:${PAD_MM}mm;overflow:hidden;font-size:${BODY_PT}pt}
.r{white-space:pre}
.h{font-size:${HEAD_PT}pt;font-weight:700}
.l{font-size:${BODY_PT}pt}
.m{font-family:${MONO_FAMILY};font-size:${MONO_PT}pt}
.k{font-style:normal;font-weight:700}
.g{display:grid;grid-template-columns:repeat(${GRID_COLS},1fr);grid-auto-rows:${GRID_ROW_MM}mm}
.c{font-family:${MONO_FAMILY};font-size:${GRID_PT}pt;border-bottom:0.2mm solid #000;margin-right:1mm;white-space:nowrap}`;

function blockHtml(p: Placed): string {
  const { spec, block } = p;
  const b = block.box;
  const style = `left:${b.x_mm}mm;top:${b.y_mm}mm;width:${b.w_mm}mm;height:${b.h_mm}mm`;
  const step = block.step_id === undefined ? "" : ` data-step="${esc(block.step_id)}"`;
  const lines = spec.lines.map((l, i) => p.rows[i].map((r, j) => {
    const head = i === 0 && j === 0 ? `<b class="h">${esc(spec.heading)}</b> ` : "";
    const mark = r.cont ? '<i class="k">»</i>' : "";
    const body = `${esc(r.text)}${mark}`;
    const cls = l.mono === true ? "m" : "l";
    return `<div class="r ${i === 0 && j === 0 ? "l" : cls}">${head}${i === 0 && j === 0 && l.mono === true ? `<span class="m">${body}</span>` : body}</div>`;
  }).join("")).join("");
  const grid = spec.grid === undefined ? "" : `<div class="g">${spec.grid.map((c) => `<div class="c">${esc(c)}</div>`).join("")}</div>`;
  return `<section class="blk" data-kind="${block.kind}" data-order="${block.order}"${step} style="${style}">${lines}${grid}</section>`;
}

export function renderHtml(placed: readonly Placed[], lang: string): string {
  const pages = (["front", "back"] as const)
    .map((pg) => `<div class="page" data-page="${pg}">${placed.filter((p) => p.block.page === pg).map(blockHtml).join("")}</div>`)
    .join("");
  return `<!doctype html><html lang="${esc(lang)}"><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${CSP}"><title>Sitate-backup</title><style>${fontCss()}${CSS}</style></head><body>${pages}</body></html>`;
}
