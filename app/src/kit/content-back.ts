// 紙の裏(「裏面」の欄)の中身。事業者別の要点は事業者の種別で引き、無い事業者は手順書の事業者別付録を参照する 1 文だけを刷る(spec 4.7、3.7)。
import type { BlockSpec, Line } from "./model.ts";
import type { KitFields } from "./types.ts";

const text = (t: string): Line => ({ t });

function providerLines(f: KitFields): Line[] {
  const h = f.provider_hints;
  if (h.kind === "b2") {
    return [
      text("アプリも配布元も無いときに、Backblaze B2 のコンソールで行うこと。"),
      text(`1. 読み取り鍵の発行: Application Keys で新しい鍵を作る。範囲はバケット ${f.storage.bucket} とプレフィクス ${f.storage.prefix}、権限は読み取りだけ、名前は ${h.key_name}(製品の印)。発行した鍵の ID と秘密を手順 (3) で使う。`),
      text(`2. 核とアダプタの取得: バケットの ${h.bin_prefix} の下(置き場 ${f.bin_path})からファイルを落とす。`),
      text(`3. 失効: 復元が終わったら ${h.key_name} の名前の鍵をコンソールで削除する。`),
    ];
  }
  return [text("この事業者の要点はこの紙に無い。手順書の付録「事業者別」を見ること。")];
}

export function backBlocks(f: KitFields): BlockSpec[] {
  return [
    { kind: "provider_notes", page: "back", heading: "裏面: 事業者別の要点", lines: providerLines(f) },
    { kind: "production_restore_notes", page: "back", heading: "裏面: 本番復元の要点",
      lines: [
        text("(1) DB は新しいクラスタに戻す。"),
        text("(2) 読む前に globals.sql から CREATE TABLESPACE を外し、SUPERUSER・REPLICATION・BYPASSRLS・CREATEROLE を持つロールと使われていないロールを消す(sitate-ro は残す)。"),
        text("(3) db.dump は所有者を保ったまま pg_restore --no-tablespaces で読み、LOGIN を持つロールに新しいパスワードを設定する。"),
        text("手順の全体は手順書の付録「本番復元」。製品の外の人の作業で、業務アプリのベンダーや外部の IT 事業者に頼んでよい。"),
      ] },
  ];
}
