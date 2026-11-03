// 承認カードの決定の受け口(主プロセス)。いま開いているカードの changeId と合う決定だけを受け、形の崩れた決定は捨てる。
// 画面の決定は IPC で届くので信用せず、見ていたカードと別の変更への決定が署名に回らないようにする(spec 3.7)。
import type { CardDecision } from "./ipc-channels.ts";

const isDecision = (d: unknown): d is CardDecision => {
  if (typeof d !== "object" || d === null) return false;
  const x = d as { kind?: unknown; proofs?: unknown };
  if (x.kind === "cancel" || x.kind === "close") return true;
  if (x.kind !== "approve" || typeof x.proofs !== "object" || x.proofs === null || Array.isArray(x.proofs)) return false;
  return Object.values(x.proofs).every((v) => typeof v === "string");
};

export class CardGate {
  #cur: { readonly changeId: string; readonly resolve: (d: CardDecision) => void } | null = null;

  /** カードを開いて決定を待つ。前のカードが残っていれば「閉じる」で解く。 */
  open(changeId: string): Promise<CardDecision> {
    this.#cur?.resolve({ kind: "close" });
    return new Promise((resolve) => { this.#cur = { changeId, resolve }; });
  }

  /** 画面からの決定。受けたら true。 */
  decide(changeId: unknown, d: unknown): boolean {
    const c = this.#cur;
    if (c === null || changeId !== c.changeId || !isDecision(d)) return false;
    this.#cur = null;
    c.resolve(d);
    return true;
  }
}
