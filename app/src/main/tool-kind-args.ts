// 種別を取る道具(sitate_propose)の kind と引数の組み合わせの検査(spec 3.7「種別を取るサブコマンド」の kind ごとの oneOf の意味)。
// 関数の名前と型は U0-core が固定し、中身は WP U3a。主プロセスが実行の前に呼び、合わなければ実行せず道具の結果でエラーを返す。
// 骨格は NOT_IMPLEMENTED を投げる。app/test/contract/tools.test.ts が組み合わせのベクトルを通して照らす。

export type KindArgsResult =
  | { readonly ok: true }
  | { readonly ok: false; readonly code: "TOOL_ARG_INVALID" };

/**
 * kind が許可リストの種別で、args がその種別の引数の組(必須の引数があり、ほかの種別の引数が無い)なら ok。
 * 共通の引数 run_id・dry_run・secret_refs はどの種別でも受ける。許可リスト外の kind も TOOL_ARG_INVALID。
 *
 * 見るのは引数の名前の組だけで、値の中身(restore や source の内部の形、note の文面)は見ない。中身は核が検査する(内部のスキーマまで写した実装は、
 * 核の検査と二重になり、核が許す値を拒む)。例外は attest の step: 署名を要さない 3 段(admin_access_opened・admin_access_closed・dest_disposed)は
 * 提案の対象でないので TOOL_ARG_INVALID(道具 sitate_attest が受けない段を kind 経由で通さない。契約試験のベクトルが照らす)。
 * 選択肢が複数ある種別は spec 3.1.2.9 の種別の表の行どおりに U3a が書く。契約試験のベクトルは、表で必須の引数が一意に決まる種別だけを照らす。
 * U3a が足す種別(ベクトルに無い): storage-init、restore-elsewhere、restore-session-extend、restore-dest-release、storage-read-extra。
 * 例: restore-session-extend は {session, hold_hours} か restore の引数、storage-read-extra は restore の引数か {force: true}。
 * この 5 種の表の行を読んで、他の種別の引数を拒むベクトルと正の例を U3a の試験に足すこと。
 */
export function checkKindArgs(kind: string, args: Readonly<Record<string, unknown>>): KindArgsResult {
  void kind;
  void args;
  throw new Error("NOT_IMPLEMENTED: checkKindArgs (U3a。spec 3.7)");
}
