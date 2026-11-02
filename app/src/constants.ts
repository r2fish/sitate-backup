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

/** LLM API の既定のエンドポイント(spec 2.4)。環境変数 ANTHROPIC_BASE_URL に頼らず、SDK へ明示して渡す。 */
export const LLM_API_BASE_URL = "https://api.anthropic.com";

/** 秘密の参照名(secret_refs の要素と *_ref の値)の字種。改行・引用符・制御文字を含められない。道具の定義の pattern と同じ。 */
export const REF_NAME_PATTERN = "^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$";

/** bootstrap の port を省いたときの既定(spec 3.7 の `port`、既定 22)。 */
export const DEFAULT_SSH_PORT = 22;
