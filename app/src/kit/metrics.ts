// 紙 1 枚(A4 縦)の寸法と文字の大きさ。配置の計算と HTML の書き出しが同じ値を使う。値は印刷物の設計値で、性能・費用の値ではない。
export const PAPER_W_MM = 210;
export const PAPER_H_MM = 297;
export const MARGIN_MM = 6;
export const GAP_MM = 0.4;
export const PAD_MM = 0.8;
export const PT_MM = 0.3528;
export const BODY_PT = 5.8;
export const MONO_PT = 5.3;
export const HEAD_PT = 6.4;
export const LINE_RATIO = 1.2;
/** 文字幅の見積り(em)。全角は 1。等幅の半角は実際の等幅フォントの最大(約 0.6)より大きい値、比例の半角は字の種類ごとに余裕を持たせる。折り返しは見積りの幅で行うので、実描画より狭く数えない。 */
export const HALF_EM = 0.62;
export const PROP_EM = 0.62;
export const PROP_UPPER_EM = 0.72;
export const PROP_WIDE_EM = 1;
export const PROP_SPACE_EM = 0.3;
/** 語の途中で折った行の末尾に刷る印(↩)の幅。 */
export const MARK_EM = 1;
export const WIDTH_SAFETY = 0.97;
export const GRID_COLS = 8;
export const GRID_ROW_MM = 5.2;
export const GRID_PT = 8.5;
