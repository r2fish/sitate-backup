// mp-app B2: 承認カードの決定は、いま開いているカードの changeId と合うものだけを受ける(別のカードへの決定や、形の崩れた決定は捨てる)。
import assert from "node:assert/strict";
import { test } from "node:test";
import { CardGate } from "../../src/main/card-gate.ts";

test("[mp-app B2] 開いているカードの changeId と合う決定だけが解け、そのあと同じ決定は効かない", async () => {
  const g = new CardGate();
  const p = g.open("c-1");
  assert.equal(g.decide("c-2", { kind: "approve", proofs: {} }), false, "別の変更への決定は捨てる");
  assert.equal(g.decide("c-1", { kind: "approve", proofs: { a: 1 } }), false, "確認語が文字列でない決定は捨てる");
  assert.equal(g.decide("c-1", { kind: "other" }), false);
  assert.equal(g.decide("c-1", { kind: "approve", proofs: { a: "x y z" } }), true);
  assert.deepEqual(await p, { kind: "approve", proofs: { a: "x y z" } });
  assert.equal(g.decide("c-1", { kind: "cancel" }), false, "解けた後は効かない");
});

test("[mp-app B2] 次のカードを開くと、前の待ちは「閉じる」で解ける", async () => {
  const g = new CardGate();
  const p1 = g.open("c-1");
  const p2 = g.open("c-2");
  assert.deepEqual(await p1, { kind: "close" });
  g.decide("c-2", { kind: "cancel" });
  assert.deepEqual(await p2, { kind: "cancel" });
});
