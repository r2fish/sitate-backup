// 記録(作業指示書 5.14 の完了条件 12、GB-126): 1 件 1 行の JSON。時刻と種類を持つ。引数の値・参照名・秘密は出さない。
// 記録を書く直前に、人の役の秘密の値(そのままの文字列と base64 の表記)が含まれていないかを検査し、含まれていれば値を伏せて `secret_redacted` を数える(防御。通常は 0)。
import { appendFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";

export class Recorder {
  readonly #file: string;
  readonly #needles: string[];
  redactions = 0;
  readonly counts = new Map<string, number>();

  constructor(outDir: string, secrets: readonly string[], fileName = "record.jsonl") {
    mkdirSync(outDir, { recursive: true });
    this.#file = join(outDir, fileName);
    this.#needles = secrets.flatMap((s) => [s, Buffer.from(s, "utf8").toString("base64")]).filter((s) => s.length >= 8);
  }

  get path(): string { return this.#file; }

  write(kind: string, fields: Record<string, unknown> = {}): void {
    this.counts.set(kind, (this.counts.get(kind) ?? 0) + 1);
    appendFileSync(this.#file, this.#clean(JSON.stringify({ at: new Date().toISOString(), kind, ...fields })) + "\n");
  }

  #clean(line: string): string {
    let out = line;
    for (const n of this.#needles) {
      // JSON の文字列としての表記(改行は \n)でも探す。
      for (const form of new Set([n, JSON.stringify(n).slice(1, -1)])) {
        while (out.includes(form)) { out = out.replace(form, "<redacted>"); this.redactions += 1; }
      }
    }
    return out;
  }

  count(kind: string): number { return this.counts.get(kind) ?? 0; }
}
