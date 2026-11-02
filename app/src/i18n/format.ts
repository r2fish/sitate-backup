// メッセージカタログの値(ICU MessageFormat の 3 形: {名前}・{名前, number}・{名前, date, short})の描画。
// 引数の欠け・型違いは例外(画面に未置換の波括弧を出さない)。
const TOKEN = /\{([a-z][a-z0-9_]*)(?:, (number|date, short))?\}/g;

export type Catalog = Readonly<Record<string, string>>;
export type FormatArgs = Readonly<Record<string, string | number | Date>>;

export function format(template: string, args: FormatArgs = {}, locale = "ja"): string {
  return template.replace(TOKEN, (_m, name: string, kind?: string) => {
    const v = args[name];
    if (v === undefined) throw new Error(`カタログの引数が無い: ${name}`);
    if (kind === undefined) return String(v);
    if (kind === "number") {
      if (typeof v !== "number") throw new Error(`数でない引数: ${name}`);
      return new Intl.NumberFormat(locale).format(v);
    }
    if (!(v instanceof Date)) throw new Error(`日付でない引数: ${name}`);
    return new Intl.DateTimeFormat(locale, { dateStyle: "short", timeStyle: "short" }).format(v);
  });
}

/** 文の描画関数。キーが無ければ例外(カタログの網羅は check.ts が建てる前に保証する)。 */
export function translator(catalog: Catalog, locale = "ja"): (key: string, args?: FormatArgs) => string {
  return (key, args) => {
    const t = catalog[key];
    if (t === undefined) throw new Error(`カタログに無いキー: ${key}`);
    return format(t, args, locale);
  };
}
