// 画面に出すモデルの文の引用の印 `[[<サブコマンド>:<JSON Pointer>=<値>]]` を、人に読める書式の値に置き換える(spec 3.7「一致」。照合を通った文だけに掛ける)。
// 日付は現地時刻、大きさは単位付き、金額は円、真偽は固定文。書式は欄の名前(JSON Pointer の末尾)と値の形で決め、判断がつかないものは値をそのまま出す。
const MARK = /\[\[([^:[\]]+):(\/[^=\]]*)=([^\]]*)\]\]/g;
const ISO = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2})?(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;

const bytes = (n: number): string => {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1000 && i < units.length - 1) { v /= 1000; i += 1; }
  return `${new Intl.NumberFormat("en", { maximumFractionDigits: 1 }).format(v)} ${units[i]!}`;
};

function formatOne(pointer: string, value: string, t: (k: string) => string, timeZone: string | undefined): string {
  const last = pointer.split("/").pop() ?? "";
  if (value === "true" || value === "false") return t(value === "true" ? "card.value.true" : "card.value.false");
  if (ISO.test(value) && !Number.isNaN(Date.parse(value))) return new Intl.DateTimeFormat("ja-JP", { dateStyle: "long", timeStyle: "short", ...(timeZone === undefined ? {} : { timeZone }) }).format(new Date(value));
  if (/^\d+$/.test(value) && /bytes$/.test(last)) return bytes(Number(value));
  if (/^\d+$/.test(value) && /(jpy|yen)/.test(last)) return `${new Intl.NumberFormat("en").format(Number(value))} 円`;
  return value;
}

export function renderMarks(text: string, t: (key: string) => string, timeZone?: string): string {
  return text.replace(MARK, (_m, _sub: string, pointer: string, value: string) => formatOne(pointer, value, t, timeZone));
}

/** renderMarks と同じ置き換えを、文の断片の列で返す(value が真の断片は置き換えた値。画面の書式の対象にしない範囲)。 */
export function renderMarksParts(text: string, t: (key: string) => string, timeZone?: string): { text: string; value: boolean }[] {
  const parts: { text: string; value: boolean }[] = [];
  let at = 0;
  for (const m of text.matchAll(MARK)) {
    if (m.index > at) parts.push({ text: text.slice(at, m.index), value: false });
    parts.push({ text: formatOne(m[2]!, m[3]!, t, timeZone), value: true });
    at = m.index + m[0].length;
  }
  if (at < text.length) parts.push({ text: text.slice(at), value: false });
  return parts;
}
