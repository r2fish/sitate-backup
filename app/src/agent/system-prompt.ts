// システムプロンプトの構成(spec 3.7、作業指示書 5.14 の完了条件 22・27)。
// 先頭は製品の説明書、続けて手順書、規則ファイル。3 つとも同梱物で会話の間に変わらないので、キャッシュの区切りは規則ファイルの後に 1 つ置く。
// 表示言語・日付・モデル名など変わる値は区切りの後ろ。
import type { AgentBundle, VolatileContext } from "./protocol.ts";

export interface SystemBlock {
  readonly type: "text";
  readonly text: string;
  readonly cache_control?: { readonly type: "ephemeral" };
}

export function buildSystemBlocks(bundle: AgentBundle, volatile: VolatileContext): SystemBlock[] {
  return [
    { type: "text", text: bundle.manual },
    { type: "text", text: bundle.procedure },
    { type: "text", text: bundle.rules, cache_control: { type: "ephemeral" } },
    { type: "text", text: `表示言語: ${volatile.locale}\n日付: ${volatile.date}\nモデル: ${volatile.model}` },
  ];
}
