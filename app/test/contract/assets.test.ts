import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { repoRoot } from "./helpers.ts";

// 製品の説明書の骨格(節番号と見出しの一覧)と規則ファイルの骨格(作業指示書 5.1)。本文は U4。
const manual = readFileSync(join(repoRoot, "assets/manual/manual.md"), "utf8");
const heads = [...manual.matchAll(/^(##|###) (\d+(?:\.\d+)?) (.+)$/gm)].map((m) => ({ level: m[1], num: m[2], title: m[3] }));

test("manual: sections are numbered in two levels, unique, ordered", () => {
  assert.ok(heads.length >= 9);
  assert.equal(new Set(heads.map((h) => h.num)).size, heads.length);
  let top = 0;
  for (const h of heads) {
    if (h.level === "##") {
      assert.match(h.num, /^\d+$/);
      assert.equal(Number(h.num), ++top);
    } else {
      assert.match(h.num, /^\d+\.\d+$/);
      assert.equal(h.num.split(".")[0], String(top));
    }
  }
  assert.equal(manual.split("\n").filter((l) => /^#{4,}/.test(l)).length, 0);
});

// 節の一覧(節番号と見出し)の完全一致。U4 が節を足す・見出しを変えるときは、監督がこの一覧と一緒に直す(作業指示書 5.16)。本文は照らさない。
const EXPECTED_SECTIONS = [
  "## 1 6 つの概念",
  "## 2 バックアップの流れ",
  "## 3 DB の経路",
  "### 3.1 取得と保管",
  "### 3.2 復元",
  "### 3.3 本番への戻し",
  "### 3.4 `load_verified_by` と固定文の意味",
  "## 4 承認と遅延",
  "## 5 復旧キット",
  "## 6 費用の見方",
  "## 7 答えられないこと",
  "### 7.1 費用と合否の断定",
  "### 7.2 法的助言",
  "### 7.3 他製品との優劣",
  "## 8 保持の決まり",
  "### 8.1 日次 90 日・月次 12 か月で、v1 では変えられないこと",
  "### 8.2 それより長く残す必要があるとき(製品の外で行うこと)",
  "## 9 健常性チェックについて(イテレーション 1)",
  "## 10 元サーバーの調査と断った候補の扱い",
];

test("manual: the list of sections matches exactly (the body is not checked)", () => {
  const got = manual.split("\n").filter((l) => /^#{2,} /.test(l));
  assert.deepEqual(got, EXPECTED_SECTIONS);
});

// 規則ファイルの見出し(1 行目)。本文は U4 が書くので、未記入の印の有無は照らさない。
const EXPECTED_RULES: Record<string, string> = {
  "approval-card-format": "# 承認カードの書式の定義",
  "outgoing-detection": "# チャット欄の送信前の検出の規則",
  "internal-terms": "# 内部概念の語の一覧と言い換え",
  "data-section-rules": "# データの区画の扱いと答えの規則",
  "quote-check-forms": "# 引用の照合の値の語の形の一覧と問いの終わり方の一覧",
};

test("rules: the five rule files exist with their heading", () => {
  for (const [f, heading] of Object.entries(EXPECTED_RULES)) {
    const s = readFileSync(join(repoRoot, "assets/rules", f + ".md"), "utf8");
    assert.equal(s.split("\n")[0], heading, f);
  }
});
