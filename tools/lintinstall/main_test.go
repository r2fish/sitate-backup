package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 製品リポジトリの根(tools/lintinstall の 2 つ上)のデータを読む。
func root(t *testing.T) string {
	t.Helper()
	r, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r, "internal/contract/schema/cli/subcommands.json")); err != nil {
		t.Fatal(err)
	}
	return r
}

const head = "# x\n## 開始前に用意するもの\n## 省けない手順\n## 確認項目\n## 手順のやり直し\n\n| 手順 | 目的 | 実行するもの | 完了確認コマンド | 期待する値 | 失敗時 |\n|---|---|---|---|---|---|\n"

func lint(t *testing.T, rows string) []string {
	t.Helper()
	p, notImpl, err := lintDoc(root(t), head+rows, false)
	if err != nil || notImpl {
		t.Fatalf("err=%v notImpl=%v", err, notImpl)
	}
	return p
}

func wantProblem(t *testing.T, ps []string, sub string) {
	t.Helper()
	for _, p := range ps {
		if strings.Contains(p, sub) {
			return
		}
	}
	t.Fatalf("%q を含む問題が無い: %v", sub, ps)
}

func TestValidRowsPass(t *testing.T) {
	ps := lint(t, "| 1 | 版を確かめる | `sitate version`、H1 | `sitate version` | `/result/signing_key_fp` = `{{DIST_KEY_FINGERPRINT}}` | `ARG_INVALID` → 直す |\n"+
		"| 2 | 面の操作 | 面: `sitate phrase verify` | `sitate status` | `/result/initialized` = `true` | — |\n"+
		"| 3 | 確認だけ | 確認だけ | `sitate source check` | `/result/selftest/<i>/ok` = `true` | — |\n")
	if len(ps) != 0 {
		t.Fatalf("合格するはずの行が落ちた: %v", ps)
	}
}

func TestInstallDocPasses(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(root(t), path))
	if err != nil {
		t.Fatal(err)
	}
	ps, notImpl, err := Lint(root(t), string(b))
	if err != nil || notImpl || len(ps) != 0 {
		t.Fatalf("手順書が検査に通らない: err=%v notImpl=%v %v", err, notImpl, ps)
	}
}

func TestGrammarViolations(t *testing.T) {
	wantProblem(t, lint(t, "| 1 | a | `sitate approve` | `sitate status` | `/result/initialized` = `true` | — |\n"), "公開区分は ui-only")
	wantProblem(t, lint(t, "| 1 | a | `sitate nosuch` | `sitate status` | `/result/initialized` = `true` | — |\n"), "CLI スキーマに無い")
	wantProblem(t, lint(t, "| 1 | a | 好きにする | `sitate status` | `/result/initialized` = `true` | — |\n"), "文法")
	wantProblem(t, lint(t, "| 1 | a | `sitate def validate --nosuch` | `sitate status` | `/result/initialized` = `true` | — |\n"), "--nosuch")
}

func TestExpectedAndFailures(t *testing.T) {
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate status` | `/result/not_in_table` = `1` | — |\n"), "に載っていない")
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate status` | initialized が true | — |\n"), "の形でない")
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate status` | `/result/initialized` = `true` | `NO_SUCH_CODE` |\n"), "一覧に無い")
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | | `/result/initialized` = `true` | — |\n"), "完了確認コマンドが無い")
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate status` | `/result/initialized` = `true` | — |\n| 1 | b | 確認だけ | `sitate status` | `/result/initialized` = `true` | — |\n"), "重複")
}

func TestMissingHeading(t *testing.T) {
	p, _, err := lintDoc(root(t), "| 手順 | 目的 | 実行するもの | 完了確認コマンド | 期待する値 | 失敗時 |\n|---|---|---|---|---|---|\n| 1 | a | 確認だけ | `sitate status` | `/result/initialized` = `true` | — |\n", false)
	if err != nil {
		t.Fatal(err)
	}
	wantProblem(t, p, "必須の見出し")
}

func TestPointerMustExistInResultSchema(t *testing.T) {
	// 送る欄の表には載っているが、そのサブコマンドの結果スキーマに無い Pointer(別のサブコマンドの欄)を落とす。
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate version` | `/result/initialized` = `true` | — |\n"), "結果スキーマ")
	// 入れ子と配列の要素(<i>)は辿る。
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate source check` | `/result/selftest/<i>/nosuch` = `true` | — |\n"), "結果スキーマ")
	if ps := lint(t, "| 1 | a | 確認だけ | `sitate def validate` | `/result/def_hash` = `abc` | — |\n"); len(ps) != 0 {
		t.Fatalf("存在する Pointer が落ちた: %v", ps)
	}
}

func TestDefNameIsSnakeCase(t *testing.T) {
	for in, want := range map[string]string{"def validate": "def_validate", "init-storage": "init_storage", "restore-dest enroll": "restore_dest_enroll", "version": "version"} {
		if got := defName(in); got != want {
			t.Errorf("%q -> %q (%q が必要)", in, got, want)
		}
	}
}

// 手順 1〜13 の形(spec 3.8 の手順の表の各行が使う道具・完了確認・Pointer の組み立て)で書いた合格の行。文は写さず、形だけを見本にする。
const stepsShape = `| 1 | 道具の配置 | ` + "`bootstrap`" + ` | ` + "`sitate version` と `sitate doctor`" + ` | 1 つ目: ` + "`/ok` = `true`、`/result/signing_key_fp` = `{{DIST_KEY_FINGERPRINT}}`" + `。2 つ目: ` + "`/result/passed` = `true`" + `(注記(入れ子)) | ` + "`SSH_UNREACHABLE`、`BOOTSTRAP_PACKAGE_FAILED`" + ` |
| 2 | 保管先の事前確認 | Q1、` + "`sitate init-storage --dry-run --endpoint ... --bucket ... --admin-ref ...`" + ` | ` + "`sitate init-storage --dry-run --endpoint ... --bucket ... --admin-ref ...`" + ` | ` + "`/result/plan/errors` = `[]`" + ` | — |
| 3 | 復元先の事前確認 | Q3、` + "`sitate restore-dest enroll --dry-run --dest ... --adapter ... --admin-ref ...`" + ` | ` + "`sitate restore-dest enroll --dry-run --dest ... --adapter ... --admin-ref ...`" + ` | ` + "`/result/plan/errors` = `[]`" + ` | — |
| 4 | 定義の用意 | Q4、` + "`sitate source survey --host ... --admin-ref ...`、`sitate estimate --endpoint ...`" + `、定義を組み立てて ` + "`sitate def validate --file -`" + ` に渡す、` + "`sitate notify set --to ... --secret-ref ...`、`sitate notify test`" + ` | ` + "`sitate def validate --file -` と `sitate notify test`" + ` | 1 つ目: ` + "`/ok` = `true`" + `。2 つ目: ` + "`/result/all_delivered` = `true`" + ` | — |
| 5 | 保管先の用意 | ` + "`sitate init-storage`" + ` | ` + "`sitate bucket check` と `sitate doctor`" + ` | 1 つ目: ` + "`/result/lock_mode` = `\"COMPLIANCE\"`、`/result/versioning` = `true`、`/result/permissions/<i>/actual` = `/result/permissions/<i>/expected`" + `(一部の要素を除く)。2 つ目: ` + "`/result/passed` = `true`" + ` | — |
| 6 | 初期化 | ` + "`sitate init --approval-pubkey <base64> --secret-fd ...`" + ` | ` + "`sitate status`" + ` | ` + "`/result/initialized` = `true`" + ` | — |
| 7 | フレーズの保管 | Q2、H2、面: ` + "`sitate phrase verify`" + `、面: ` + "`sitate kit`" + `、面: ` + "`sitate kit confirm`" + ` | ` + "`sitate status`" + ` | ` + "`/result/phrase_confirmed` = `true`、`/result/kit/confirmed` = `true`" + ` | — |
| 8 | 元サーバーの登録 | ` + "`sitate source enroll`" + ` | ` + "`sitate source check`" + ` | ` + "`/result/selftest/<i>/ok` = `true`、`/result/actionable` = `[]`" + `(除く種別の注記) | — |
| 9 | 復元先の登録 | ` + "`sitate restore-dest enroll`" + ` | ` + "`sitate restore-dest check`" + ` | ` + "`/result/isolation/<i>/ok` = `true`" + ` | — |
| 10 | 確定 | ` + "`sitate propose seal --file -`" + ` → H4 → ` + "`sitate apply`" + ` | ` + "`sitate def show` と `sitate status`" + ` | 1 つ目: ` + "`/result/sealed` = `true`" + `。2 つ目: ` + "`/result/notify/proof_confirmed` = `true`" + ` | ` + "`NOTIFY_PROOF_MISMATCH`" + ` → 通知先を確かめる |
| 11 | 初回の取得 | ` + "`sitate backup`" + ` | ` + "`sitate status`" + ` | ` + "`/result/last_backup/ok` = `true`" + ` | — |
| 12 | 復元の試験 | ` + "`sitate restore-test`" + ` | ` + "`sitate status`" + ` | ` + "`/result/last_restore_test/ok` = `true`" + ` | — |
| 13 | タイマーの確認 | 確認だけ | ` + "`sitate status` と `sitate doctor`" + ` | 1 つ目: ` + "`/result/schedule` = `<定義の schedule>`、`/warnings/<i>/code` ⊆ `{{WARNING_ALLOWLIST}}`" + `。2 つ目: ` + "`/result/passed` = `true`" + ` | — |
`

func TestSteps1to13ShapePass(t *testing.T) {
	if ps := lint(t, stepsShape); len(ps) != 0 {
		t.Fatalf("手順 1〜13 の形が不合格(%d 件): %v", len(ps), ps)
	}
}

// 骨格の記法の見本の表は、未実装の印を外せば合格する(U4 が見本をそのまま真似られる)。

func TestCompletionCommandsAreSplitPerPointer(t *testing.T) {
	two := "| 1 | a | 確認だけ | `sitate version` と `sitate doctor` | "
	wantProblem(t, lint(t, two+"`/ok` = `true`、`/result/passed` = `true` | — |\n"), "2 個のとき")
	// 1 つ目の Pointer は 1 つ目のコマンドの結果スキーマで照らす(doctor の欄を version の側に書くと落ちる)。
	wantProblem(t, lint(t, two+"1 つ目: `/result/passed` = `true`。2 つ目: `/result/passed` = `true` | — |\n"), "結果スキーマ")
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate status` | 1 つ目: `/result/initialized` = `true` | — |\n"), "n つ目")
	wantProblem(t, lint(t, two+"2 つ目: `/ok` = `true`。1 つ目: `/ok` = `true` | — |\n"), "順")
}

func TestFormerFalseRejections(t *testing.T) {
	ok := func(row string) {
		t.Helper()
		if ps := lint(t, row); len(ps) != 0 {
			t.Fatalf("合格するはずの行が落ちた: %v", ps)
		}
	}
	ok("| 1 | a | 定義を組み立てて `sitate def validate --file -` に渡す | `sitate status` | `/result/initialized` = `true` | — |\n")
	ok("| 1 | a | `sitate init --approval-pubkey <base64> --secret-fd ...` | `sitate status` | `/result/initialized` = `true` | — |\n")
	ok("| 1 | a | 面: `sitate kit` | `sitate status` | `/result/initialized` = `true` | — |\n")
	ok("| 1 | a | 確認だけ | `sitate source check` | `/result/actionable` = `[]`(a(b)c) | — |\n")
	// 規則: アプリの原因コードは通り、無い名前は落ちる。
	ok("| 1 | a | 確認だけ | `sitate status` | `/result/initialized` = `true` | `SSH_UNREACHABLE`、`BOOTSTRAP_PACKAGE_FAILED` |\n")
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate status` | `/result/initialized` = `true` | `SSH_NOSUCH` |\n"), "一覧に無い")
	// 受けない引数・地の文だけの要素は引き続き落ちる。
	wantProblem(t, lint(t, "| 1 | a | `sitate status --nosuch` | `sitate status` | `/result/initialized` = `true` | — |\n"), "--nosuch")
}

func TestPointerInsideClosedAllOfIsRejected(t *testing.T) {
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate doctor` | `/result/checks/<i>/zzz` = `true` | — |\n"), "結果スキーマ")
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate backup` | `/result/checks/<i>/zzz` = `true` | — |\n"), "結果スキーマ")
}

// --- 指摘 19: 印の許可リスト・手順 1 の指紋・手順 1〜13 の有無・見出し違いの表・失敗時が空 ---

// 手順 1〜13 の見本から、手順 n の行を除いた文書(完全な検査 Lint を通す)。
func stepsDoc(without ...string) string {
	var rows []string
	for _, l := range strings.Split(stepsShape, "\n") {
		drop := l == ""
		for _, w := range without {
			drop = drop || strings.HasPrefix(l, "| "+w+" |")
		}
		if !drop {
			rows = append(rows, l)
		}
	}
	return head + strings.Join(rows, "\n") + "\n"
}

func lintFull(t *testing.T, doc string) []string {
	t.Helper()
	p, notImpl, err := Lint(root(t), doc)
	if err != nil || notImpl {
		t.Fatalf("err=%v notImpl=%v", err, notImpl)
	}
	return p
}

func TestFullDocWithAllStepsPasses(t *testing.T) {
	if ps := lintFull(t, stepsDoc()); len(ps) != 0 {
		t.Fatalf("手順 1〜13 がそろった文書が不合格: %v", ps)
	}
}

func TestMissingStepIsRejected(t *testing.T) {
	for _, n := range []string{"1", "9", "12", "13"} {
		wantProblem(t, lintFull(t, stepsDoc(n)), "手順 "+n+" の行が無い")
	}
}

func TestMarkAllowList(t *testing.T) {
	// 綴りの誤り(本文のどこでも)、許可リストに無い名前
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate version` | `/result/signing_key_fp` = `{{DIST_KEY_FINGERPRNT}}` | — |\n"), "は許可リスト(")
	wantProblem(t, lintFull(t, stepsDoc()+"\n本文 {{OTHER_MARK}}\n"), "は許可リスト(")
	// 印は決まった Pointer・演算子の右辺だけ
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate doctor` | `/result/passed` = `{{WARNING_ALLOWLIST}}` | — |\n"), "形だけに置ける")
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate status` | `/result/schedule` = `{{WARNING_ALLOWLIST}}` | — |\n"), "形だけに置ける")
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate status` | `/result/schedule` = `{{TIMER_NAMES}}` | — |\n"), "は許可リスト(")
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate doctor` | `/result/passed` = `{{DIST_KEY_FINGERPRINT}}` | — |\n"), "形だけに置ける")
}

func TestStep13ScheduleCheck(t *testing.T) {
	// 定義の schedule との照合が消えた手順 13 を落とす(空虚な合格。D-0168)
	wantProblem(t, lintFull(t, strings.Replace(stepsDoc(), "`/result/schedule` = `<定義の schedule>`、", "", 1)), "手順 13 の期待する値")
	wantProblem(t, lintFull(t, strings.Replace(stepsDoc(), "`/result/schedule` = `<定義の schedule>`", "`/result/schedule` = `02:30`", 1)), "手順 13 の期待する値")
}

func TestFingerprintOfStep1(t *testing.T) {
	// 指紋の直書き・別の印・印の無い手順 1 を落とす
	wantProblem(t, lint(t, "| 2 | a | 確認だけ | `sitate version` | `/result/signing_key_fp` = `SHA256:abcdefghijklmnopqrstuvwxyzabcdefghijklmnopq` | — |\n"), "指紋の直書き")
	wantProblem(t, lint(t, "| 2 | a | 確認だけ | `sitate version` | `/result/signing_key_fp` = `{{WARNING_ALLOWLIST}}` | — |\n"), "指紋の直書き")
	wantProblem(t, lintFull(t, strings.Replace(stepsDoc(), "`/result/signing_key_fp` = `{{DIST_KEY_FINGERPRINT}}`", "`/result/version` = `1.0.0`", 1)), "手順 1 の期待する値")
	wantProblem(t, lintFull(t, strings.Replace(stepsDoc(), "`/result/signing_key_fp` = `{{DIST_KEY_FINGERPRINT}}`", "`/ok` = `true`", 1)), "手順 1 の期待する値")
}

func TestHeaderMismatchTableIsRejected(t *testing.T) {
	// 付録を spec と同じ 5 列で書く・見出しを一字変える、と表が読まれず素通りする。見出しの違う表を落とす。
	wantProblem(t, lintFull(t, stepsDoc()+"\n| 手順 | 目的 | 実行するもの | 完了確認コマンド | 期待する値 |\n|---|---|---|---|---|\n| V1 | a | b | c | d |\n"), "見出しが手順の表と違う")
	wantProblem(t, lintFull(t, stepsDoc()+"\n| 手順 | 目的 | 実行するもの | 完了確認コマンド | 期待する値 | 失敗した時 |\n|---|---|---|---|---|---|\n"), "見出しが手順の表と違う")
	// 関係の無い表は落とさない
	if ps := lintFull(t, stepsDoc()+"\n| 項目 | 内容 |\n|---|---|\n| a | b |\n"); len(ps) != 0 {
		t.Fatalf("関係の無い表が落ちた: %v", ps)
	}
}

func TestEmptyFailureCellIsRejected(t *testing.T) {
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate status` | `/result/initialized` = `true` |  |\n"), "失敗時が空")
}

func TestAppCodesFollowTheTable(t *testing.T) {
	// spec 3.7 の表: キー app.tool.human_cancelled などは TOOL_ を付けない。実在しない TOOL_HUMAN_CANCELLED は落ちる。
	for _, c := range []string{"HUMAN_CANCELLED", "PATH_REF_UNKNOWN", "QUOTE_REJECTED", "TOOL_DENIED", "TOOL_ARG_INVALID", "SSH_DISCONNECTED", "LLM_BILLING", "AGENT_EXITED", "ENVELOPE_EXIT_UNKNOWN"} {
		if ps := lint(t, "| 1 | a | 確認だけ | `sitate status` | `/result/initialized` = `true` | `"+c+"` |\n"); len(ps) != 0 {
			t.Errorf("%s が落ちた: %v", c, ps)
		}
	}
	for _, c := range []string{"TOOL_HUMAN_CANCELLED", "TOOL_PATH_REF_UNKNOWN", "TOOL_QUOTE_REJECTED"} {
		wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate status` | `/result/initialized` = `true` | `"+c+"` |\n"), "一覧に無い")
	}
}

func TestFileFlagTakesStdinOnly(t *testing.T) {
	wantProblem(t, lint(t, "| 1 | a | `sitate def validate --file /srv/x.yaml` | `sitate status` | `/result/initialized` = `true` | — |\n"), "--file は `-`")
	wantProblem(t, lint(t, "| 1 | a | 確認だけ | `sitate def validate --file x` | `/result/def_hash` = `abc` | — |\n"), "--file は `-`")
}
