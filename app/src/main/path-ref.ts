// path_ref の対応表(spec 3.7 の「道具の定義の写像」の path_ref の行と送る欄の表)。
// 値から導かない乱数で参照名を作り、対応は主プロセスのメモリだけに置く。アプリの終了で捨てる(永続化しない)。
import { randomBytes } from "node:crypto";

export const PATH_REF_PATTERN = /^pr:[0-9a-f]{16}$/;
const PATH_REF_RANDOM_BYTES = 8;
const PATH_REF_PREFIX = "pr:";

export class PathRefUnknownError extends Error {
  constructor() {
    super("PATH_REF_UNKNOWN");
  }
}

export class PathRefTable {
  private readonly byValue = new Map<string, string>();
  private readonly byRef = new Map<string, string>();

  /** 元サーバー由来のパスや DB 名に参照名を割り当てる。同じ会話の同じ値には同じ参照名を返す。 */
  refFor(value: string): string {
    const known = this.byValue.get(value);
    if (known !== undefined) return known;
    let ref = "";
    do ref = PATH_REF_PREFIX + randomBytes(PATH_REF_RANDOM_BYTES).toString("hex");
    while (this.byRef.has(ref));
    this.byValue.set(value, ref);
    this.byRef.set(ref, value);
    return ref;
  }

  /** 参照名を元の値に戻す。知らない参照名は undefined。 */
  resolve(ref: string): string | undefined {
    return this.byRef.get(ref);
  }

  /** 表示のために、文字列に含まれる参照名を実値に置き換える(画面のプロセスへ渡す前の描画用。モデルへ送る経路では使わない)。 */
  renderText(text: string): string {
    return text.replace(/pr:[0-9a-f]{16}/g, (m) => this.byRef.get(m) ?? m);
  }

  /**
   * 入力のどの深さの文字列でも、値全体が参照名なら元の値に戻す。`pr:` で始まり対応表に無い値(`pr:<16 桁>/sub` を含む)は PathRefUnknownError。
   * 戻した値を返す(入力は書き換えない)。
   */
  restoreDeep<T>(input: T): T {
    return this.walk(input) as T;
  }

  private walk(v: unknown): unknown {
    if (typeof v === "string") return this.restoreString(v);
    if (Array.isArray(v)) return v.map((x) => this.walk(x));
    if (typeof v === "object" && v !== null) return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, this.walk(x)]));
    return v;
  }

  private restoreString(s: string): string {
    if (!s.startsWith(PATH_REF_PREFIX)) return s;
    const real = PATH_REF_PATTERN.test(s) ? this.byRef.get(s) : undefined;
    if (real === undefined) throw new PathRefUnknownError();
    return real;
  }
}
