// RFC 8785(JCS)の直列化。承認の正規化ハッシュ(spec 4.6)を核と同じ手順で作る。
// 数の表記は ECMAScript の Number→文字列(JCS が採る規則)で、JSON.stringify と一致する。キーは UTF-16 コード単位の昇順。

export function jcs(value: unknown): string {
  if (value === null || typeof value === "boolean" || typeof value === "string") return JSON.stringify(value);
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new Error("JCS: 有限でない数");
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return "[" + value.map(jcs).join(",") + "]";
  if (typeof value === "object") {
    const o = value as Record<string, unknown>;
    const keys = Object.keys(o).filter((k) => o[k] !== undefined).sort();
    return "{" + keys.map((k) => JSON.stringify(k) + ":" + jcs(o[k])).join(",") + "}";
  }
  throw new Error("JCS: 直列化できない型");
}
