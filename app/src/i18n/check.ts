// メッセージカタログの形(作業指示書 5.1 の U0 固定一覧 GB-124)と検査の入口。U0-core の持ち物。U3a・U3b は変えない。
//
// - ファイル形式: `ja/catalog.json`・`en/catalog.json` は、キー(文字列)→ 値(文字列)の平らな JSON の 1 つのオブジェクト。
// - キーの一覧と置換変数の引数: `keys.json`(キー → { args: { 引数名: 型 } })。ビルドの検査は、日英のカタログのキーがこの一覧と一致することを求める。
// - キーの名前空間(先頭の語): core.code.<原因コード>、core.why.<why のコード>(核。コードの一覧は CLI スキーマ)、
//   core.reason.<原因コード>.<detail.reason の値>(reason ごとの案内が要るものだけ。今は 2 件。設計の監督の D-0157 の申し送りで U0 が形を決めた)、
//   app.<系>.<名前>(アプリの原因コードの列挙。核の名前空間と別)、card.*(承認カード)、chat.*(チャット欄)、
//   status.*(状態と報告の表示)、phrase.*(フレーズのパネル)。
// - 値は ICU MessageFormat。置換変数は {名前}(string)、{名前, number}(number)、{名前, date, short}(date)の 3 つに限る。
//   引数名は小文字の snake_case。型は keys.json の args と一致させる(日英とも同じ集合)。
// - 仮の文の印: 日本語は先頭が「[仮] 」、英語は先頭が「[tmp] 」。本物の文に置き換えると印が消える。
// - 擬似ロケール: pseudo()。画面崩れの機械判定: layoutProblems()。

export type ArgType = "string" | "number" | "date";
export const PLACEHOLDER_MARK = { ja: "[仮] ", en: "[tmp] " } as const;
export const PSEUDO_EXPANSION = 1.4;
/** 画面崩れの判定の上限(本物の文のとき。文字数)。見出し・ラベル・操作名の類と、それ以外。 */
export const LAYOUT_MAX_SHORT = 40;
export const LAYOUT_MAX_LONG = 400;
const ARG_TOKEN = /\{([a-z][a-z0-9_]*)(?:, (number|date, short))?\}/g;

export function isPlaceholder(value: string): boolean {
  return value.startsWith(PLACEHOLDER_MARK.ja) || value.startsWith(PLACEHOLDER_MARK.en);
}

/** 値の置換変数を { 引数名: 型 } で返す。書式の外の波括弧があれば例外にする。 */
export function parseArgs(value: string): Record<string, ArgType> {
  const out: Record<string, ArgType> = {};
  const rest = value.replace(ARG_TOKEN, (_m, name: string, kind?: string) => {
    out[name] = kind === undefined ? "string" : kind === "number" ? "number" : "date";
    return "";
  });
  if (/[{}]/.test(rest)) throw new Error(`置換変数の書式の外の波括弧: ${value}`);
  return out;
}

const ACCENT: Record<string, string> = {
  a: "á", e: "é", i: "í", o: "ó", u: "ú", c: "ç", n: "ñ", y: "ý",
  A: "Á", E: "É", I: "Í", O: "Ó", U: "Ú", C: "Ç", N: "Ñ", Y: "Ý",
};

/** 擬似ロケールの変換: 置換変数の外の文字にアクセントを付け、PSEUDO_EXPANSION 倍に伸ばし、⟦ ⟧ で囲む。 */
export function pseudo(value: string): string {
  const parts = value.split(/(\{[^{}]*\})/);
  const body = parts
    .map((p) => (p.startsWith("{") ? p : [...p].map((c) => ACCENT[c] ?? c).join("")))
    .join("");
  const plainLen = parts.filter((p) => !p.startsWith("{")).join("").length;
  const pad = "~".repeat(Math.ceil(plainLen * (PSEUDO_EXPANSION - 1)));
  return `⟦${body}${pad}⟧`;
}

/** 見出し・ラベル・操作名の類の短い欄。core.code.*・core.why.*(原因コードの平易な説明と次の行動の文。spec 3.7)は長い欄。 */
function isShortKey(key: string): boolean {
  return /\.(heading|title|label)$/.test(key) || /^card\.(section|actions)\./.test(key);
}

/** 5 面の画面崩れの機械判定。問題の一覧を返す(空なら合格)。仮の文は文字数の判定から外す。 */
export function layoutProblems(key: string, value: string): string[] {
  const problems: string[] = [];
  const p = pseudo(value);
  try {
    if (JSON.stringify(parseArgs(p)) !== JSON.stringify(parseArgs(value))) problems.push("擬似ロケールで置換変数が変わった");
  } catch (e) {
    problems.push(String(e));
  }
  if (isShortKey(key) && /\n/.test(value)) problems.push("1 行の欄に改行がある");
  if (!isPlaceholder(value)) {
    const max = isShortKey(key) ? LAYOUT_MAX_SHORT : LAYOUT_MAX_LONG;
    if (value.length > max) problems.push(`文字数 ${value.length} が上限 ${max} を超える`);
    if (p.length > Math.ceil(max * (PSEUDO_EXPANSION + 0.2))) problems.push("擬似ロケールで欄からあふれる");
  }
  return problems;
}
