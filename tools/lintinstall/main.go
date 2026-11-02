// Command lintinstall は docs/install.md の書式検査(作業指示書 3.2「手順書」、5.1、GB-134)。標準ライブラリだけ。
//
// 検査: 未実装の印(骨格)、必須の見出し、手順の表(列・手順番号の重複・完了確認コマンドの有無)、
// 「実行するもの」の文法(spec 3.8 の 6 種。公開区分の表を読む)、完了確認コマンドと選択肢の照合(CLI スキーマと gate の許可リスト)、
// 期待する値の書式(JSON Pointer = 値 / ⊇ / ⊆)、期待する値の Pointer が送る欄の表のデータと同じサブコマンドの結果スキーマ($defs の名前はサブコマンド名の snake_case)にあること、失敗時の原因コードが一覧にあること(空は不可。`—` は可)、
// 印の許可リスト(marks.go)、手順 1 の指紋の印、手順 1〜13 の有無、見出しの違う手順の表の拒否。
//
// 手順の表の書式(記法の字句。U0-core が固定する。U4 は従う): 本文の Markdown の表で、見出し行は
// `| 手順 | 目的 | 実行するもの | 完了確認コマンド | 期待する値 | 失敗時 |`。1 手順 1 行。
//   - 実行するもの: 「、」で区切った要素の並び。要素は `sitate <サブコマンド> …`(公開区分 tool)、`bootstrap`、`bootstrap-restore`、
//     Qn、Hn、「面: …」(`sitate …` を含むなら公開区分 tool か ui-only)、「外: …」(製品の外の人の作業)、「確認だけ」のどれか。丸括弧の注記は読まない。
//   - 完了確認コマンド: `sitate <サブコマンド> …` のコード片(公開区分 tool)。複数のときは並べ、期待する値を「1 つ目: … 2 つ目: …」で区間に分ける。
//   - 期待する値: 「、」で区切った要素の並び。要素は `<Pointer>` と演算子(=、⊇、⊆)と `値` で、Pointer は RFC 6901。`<i>` は配列の全要素。
//   - 失敗時: 原因コード(`CODE`)を含む文。コードは CLI スキーマの error.code か warnings[].code の一覧にあること。
package main

import (
	"fmt"
	"os"
)

const (
	path = "docs/install.md"
	// 骨格に置く未実装の印(docs/install.md の中の 1 行)。U4 が本文を書くときに消す。
	notImplementedMark = "<!-- sitate:not-implemented -->"
)

func main() {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("lint-install: 不合格: %v\n", err)
		os.Exit(1)
	}
	problems, notImpl, err := Lint(".", string(b))
	if err != nil {
		fmt.Printf("lint-install: 不合格: %v\n", err)
		os.Exit(1)
	}
	if notImpl {
		fmt.Println("NOT_IMPLEMENTED: docs/install.md は骨格のまま(U4 が書く)")
		os.Exit(1)
	}
	for _, p := range problems {
		fmt.Printf("lint-install: %s\n", p)
	}
	if len(problems) > 0 {
		fmt.Printf("lint-install: 不合格(%d 件)\n", len(problems))
		os.Exit(1)
	}
	fmt.Println("lint-install: 合格")
}
