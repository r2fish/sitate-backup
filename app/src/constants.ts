// アプリの定数の 1 ファイル(作業指示書 1 章の定数表の置き場 app の行)。値は定数の名前で参照し、数値を直書きしない。
// 根拠: spec 3.7(設計の初期値)。

/** 核の結果の要約をモデルへ送るときの 1 回の上限(文字数・要素数)。spec 3.7。 */
export const ENVELOPE_MAX_CHARS = 2000;
export const ENVELOPE_MAX_ITEMS = 128;
/** 「無音」とみなすバックアップの無い日数。spec 3.7。 */
export const SILENCE_BACKUP_DAYS = 2;

// 固定値の表(app の行。変える手順を持たない)。
export const SSH_KEEPALIVE_INTERVAL_S = 30;
export const SSH_KEEPALIVE_MAX = 3;
export const LLM_MAX_TOKENS = 8192;
export const TOOL_ROUNDTRIP_MAX = 20;
