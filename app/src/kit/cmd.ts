// 紙に刷るコマンド行の組み立て。argv の語をシェルに貼れる形に引用し、「オプションと値」の組を折り返しの単位にする。
import type { Line } from "./model.ts";

/** シェルに貼れる形に引用する。`$名前` を含む語は展開される二重引用符、そのほかの記号を含む語は単引用符で囲む。 */
export function shellWord(a: string): string {
  if (/^[A-Za-z0-9_@%+=:,./-]+$/.test(a)) return a;
  if (a.includes("$") && !/["`\\]/.test(a) && !/\$(?![A-Za-z_])/.test(a)) return `"${a}"`;
  return `'${a.replaceAll("'", "'\\''")}'`;
}

/** 語の並びを折り返しの単位に分ける: `-x 値` のような(ハイフンで始まる語と、続く値の)組は 1 単位。 */
export function unitsOf(words: readonly string[]): string[] {
  const out: string[] = [];
  for (let i = 0; i < words.length; i++) {
    const w = words[i];
    const next = words[i + 1];
    if (w.startsWith("-") && next !== undefined && !next.startsWith("-")) {
      out.push(`${w} ${next}`);
      i += 1;
    } else out.push(w);
  }
  return out;
}

/** 等幅のコマンド行(単位ごとに折る)。 */
export function cmdLine(units: readonly string[]): Line {
  return { t: units.join(" "), mono: true, units };
}

const STDIN_FLAGS = ["--secrets-stdin", "--phrase-stdin", "--stdin"];

/** argv のコマンド行。標準入力で秘密を受けるコマンドは、シェル変数 X から `printf` で渡す形にする(spec 4.7「秘密の標準入力」)。 */
export function argvLine(argv: readonly string[]): Line {
  const pipe = argv.some((a) => STDIN_FLAGS.includes(a)) ? ["printf '%s\\n'", '"$X"', "|"] : [];
  return cmdLine([...pipe, ...unitsOf(argv.map(shellWord))]);
}

/** 引用しない語(プレースホルダ `<…>` を含む)のコマンド行。 */
export function plainCmd(words: readonly string[]): Line {
  return cmdLine(unitsOf(words));
}
