// 印刷用の日本語フォント(Noto Sans JP。SIL OFL 1.1。fonts/OFL.txt)を HTML に埋め込む。ファイルを読まず、定数の文字列から作る(束ねても動く)。
import { FONT_JAPANESE_WOFF2_B64 } from "./font-data-japanese.ts";
import { FONT_LATIN_WOFF2_B64 } from "./font-data-latin.ts";

const LATIN_RANGE = "U+0000-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,U+2000-206F,U+2074,U+20AC,U+2122,U+2191,U+2193,U+2212,U+2215,U+FEFF,U+FFFD";
const JAPANESE_RANGE = "U+3000-30FF,U+31F0-31FF,U+FF00-FFEF,U+4E00-9FFF,U+2190-21FF,U+2460-24FF,U+25A0-25FF";

function face(b64: string, unicodeRange: string): string {
  return `@font-face{font-family:"SitateJP";src:url(data:font/woff2;base64,${b64}) format("woff2");font-weight:400;unicode-range:${unicodeRange};}`;
}

/** @font-face の CSS(欧文の字と日本語の字)。 */
export function fontCss(): string {
  return face(FONT_LATIN_WOFF2_B64, LATIN_RANGE) + face(FONT_JAPANESE_WOFF2_B64, JAPANESE_RANGE);
}
