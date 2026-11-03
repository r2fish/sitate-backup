// 道具の呼び出しに差し込む秘密の値を集める(spec 3.7 の写像)。キーチェーンに有ればそれを使い、無ければ秘密の入力ボックスを開く。
// 事業者の管理用鍵(init-storage・init・apply の admin_ref)はキーチェーンを見ず、常に保存しないモードで開く(D-0159。同名の別の資格情報を取り違えない)。
import type { Keychain } from "./keychain.ts";
import type { SecretBox } from "./secret-box.ts";
import { BUSINESS_KEY_TOOLS, KEYCHAIN_REF_ARGS, defaultArgFor, labelFor, shapeSecret } from "./secret-labels.ts";
import type { ToolPlan } from "./tool-gate.ts";

export interface SecretFlowDeps {
  readonly keychain: Keychain;
  readonly service: () => string;
  readonly secretBox?: SecretBox | undefined;
}

/** この参照名を取った引数の名前(ラベルと値の形を決める)。secret_refs だけで来たものは道具から決める(決められなければ null)。 */
const argOf = (plan: ToolPlan, ref: string): string | null => KEYCHAIN_REF_ARGS.find((k) => plan.input[k] === ref) ?? defaultArgFor(plan.tool, ref, plan.input, plan.secretRefs);

/** 人が入れた値を、道具と引数に合う形にして返す。取り消し(閉じた)なら null。 */
export async function askSecret(d: SecretFlowDeps, tool: string, arg: string | null, ref: string, mode: "save" | "no-save"): Promise<unknown> {
  if (d.secretBox === undefined) return null;
  const out = await d.secretBox.request(mode, ref, labelFor(tool, arg ?? ""));
  if (out.kind === "cancelled") return null;
  const raw = out.kind === "transient" ? out.value.take() : await d.keychain.get(d.service(), ref);
  return raw === null ? null : shapeSecret(tool, arg ?? "", raw);
}

/** plan.secretRefs の値を集める。人が閉じれば null。 */
export async function collectSecrets(d: SecretFlowDeps, plan: ToolPlan): Promise<Record<string, unknown> | null> {
  const values: Record<string, unknown> = {};
  for (const ref of plan.secretRefs) {
    const arg = argOf(plan, ref);
    const business = arg === "admin_ref" && BUSINESS_KEY_TOOLS.has(plan.tool);
    if (!business) {
      const have = await d.keychain.get(d.service(), ref);
      if (have !== null) { values[ref] = shapeSecret(plan.tool, arg ?? "", have); continue; }
    }
    const v = await askSecret(d, plan.tool, arg, ref, business ? "no-save" : "save");
    if (v === null) return null;
    values[ref] = v;
  }
  return values;
}
