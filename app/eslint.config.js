// コードの大きさの検査(作業指示書 1 章「コードの大きさ」)。上限は make lint-size の Go の分と同じ値:
// ファイル 400 行、関数 60 行、テストのファイル 600 行。設定は U0-core の持ち物で、変えるのは監督だけ。
import tseslint from "typescript-eslint";

const MAX_FILE = 400;
const MAX_TEST_FILE = 600;
const MAX_FUNCTION = 60;
const countAll = { skipBlankLines: false, skipComments: false };

export default tseslint.config(
  { ignores: ["node_modules/**", "out/**"] },
  { linterOptions: { noInlineConfig: true } }, // ファイル内の印(eslint-disable など)で上限を外せない
  {
    files: ["**/*.ts", "**/*.tsx", "**/*.mts", "**/*.mjs", "**/*.js"],
    languageOptions: { parser: tseslint.parser },
    rules: {
      "max-lines": ["error", { max: MAX_FILE, ...countAll }],
      "max-lines-per-function": ["error", { max: MAX_FUNCTION, ...countAll, IIFEs: true }],
    },
  },
  {
    files: ["**/*.test.ts", "**/*.test.tsx", "test/**"],
    rules: { "max-lines": ["error", { max: MAX_TEST_FILE, ...countAll }] },
  },
);
