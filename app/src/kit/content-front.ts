// 日本語の版の説明文と、紙の表(「裏面」以外の欄)の中身。値(ハッシュ・URL・コマンド)は `fields` から差し込み、説明文だけがここの文(spec 4.7)。
import type { BlockSpec, Line } from "./model.ts";
import type { KitFields } from "./types.ts";
import { plainCmd } from "./cmd.ts";
import { stepBlocks } from "./content-steps.ts";

const mono = (t: string): Line => ({ t, mono: true });
const text = (t: string): Line => ({ t });

export const LLM_KEY_LINE = "新しい端末ではアプリに LLM API キーを入れ直す。";

export function frontBlocks(f: KitFields, phrase: readonly string[]): BlockSpec[] {
  const st = f.storage;
  const head: BlockSpec[] = [
    { kind: "title", page: "front", heading: "Sitate-backup 復旧キット",
      lines: [mono(`キットの版 ${f.kit_version}    発行日 ${f.issued_on}`)] },
    { kind: "product", page: "front", heading: "製品",
      lines: [
        mono(`核の版 ${f.core_version}   形式の版 ${f.format_version}`),
        mono(`配布元の取得先 ${f.distribution_url}`),
        mono(`核 SHA-256 linux-x86_64 ${f.core_sha256.linux_x86_64}`),
        mono(`核 SHA-256 linux-arm64  ${f.core_sha256.linux_arm64}`),
        mono(`SBOM SHA-256 ${f.sbom_sha256}`),
        mono(`ソース SHA-256 ${f.source_sha256}`),
        mono(`保管先の bin/ の置き場 ${f.bin_path}`),
      ] },
    { kind: "installer", page: "front", heading: "製品(アプリのインストーラー)",
      lines: [
        text("インストーラーは配布元の取得先 URL から得て、並べて置いた署名 <ファイル名>.sig を下の「配布者の公開鍵」で確かめる(OS のコード署名は補助。SHA-256 は印字しない)。配布元が無いときは、アプリを使わない経路で復元する。"),
        plainCmd(["minisign", "-Vm", "<インストーラー>", "-x", "<インストーラー>.sig", "-P", "<公開鍵>"]),
      ] },
    { kind: "signing_keys", page: "front", heading: "配布者の公開鍵",
      lines: [
        ...f.signing_keys.flatMap((k): Line[] => [mono(`公開鍵 ${k.public_key}`), mono(`指紋 ${k.fp}`)]),
        text("核は SHA-256 で確かめ(minisign があれば署名も)、それ以外の版は署名で確かめる。minisign は製品の外の道具。"),
        plainCmd(["sha256sum", "sitate"]),
        plainCmd(["minisign", "-Vm", "sitate", "-x", "sitate.sig", "-P", "<公開鍵>"]),
      ] },
    { kind: "storage", page: "front", heading: "保管先",
      lines: [
        mono(`事業者 ${st.provider}   国 ${st.country ?? "(不明)"}`),
        mono(`エンドポイント ${st.endpoint}`),
        mono(`バケット ${st.bucket}   プレフィクス ${st.prefix}`),
        mono(`repo_id ${st.repo_id}   keyring_id ${st.keyring_id}   key_ref ${st.key_ref}`),
        text("保管先の読み取り資格情報はこの紙に載せない。手順 (3) で、保管先の契約者が持つ事業者の管理用鍵から発行し、手順 (6) で失効させる。保管先準備アダプタの無い事業者と、アプリも配布元も無いときは、事業者のコンソールで発行する(手順書の事業者別付録。要点は裏面)。"),
      ] },
  ];
  const tail: BlockSpec[] = [
    { kind: "restore_proof", page: "front", heading: "復元できた証明",
      lines: [text("アプリ無しで復元したときは、(5) の結果の /ok = true、/result/hash_mismatch = 0、/result/backup_ref、/result/files、/result/bytes を書き留めて、復元を行った人(または依頼先)が報告する。/result/dumps/<i>/loaded = false は DB へ読み込んでいない印。")] },
    { kind: "explanation", page: "front", heading: "本番への戻し方と LLM API キー",
      lines: [
        text("本番への戻し方: DB のダンプはロール定義 globals.sql を先に読み、LOGIN を持つロールのパスワードを設定し直す。所有者(uid / gid)と権限の戻し方を含め、手順書の付録「本番復元」を見る(要点は裏面)。製品の外の人の作業で、業務アプリのベンダーや外部の IT 事業者に頼んでよい。"),
        text(LLM_KEY_LINE),
        text("Anthropic Console で新しいキーを発行し、アプリの秘密の入力ボックスで入れる。この紙にキーは載せない。"),
      ] },
    { kind: "phrase_entry", page: "front", heading: "フレーズ(24 語。英語の単語)", grid: phrase.map((w, i) => `${i + 1}. ${w}`),
      lines: [text("刷り込むか手書きする。確認は、アプリの「フレーズの確認」で 24 語を入れ直して合格すること(phrase verify)。アプリが無いときは、手順 (4) の記録の確認(journal verify)がこの 24 語で通ることで確かめる。この紙とフレーズの写真を撮らない。")] },
    { kind: "host_key", page: "front", heading: "管理用",
      lines: [mono(`バックアップサーバーのホスト鍵の指紋 ${f.host_key_fp}`)] },
    { kind: "custody", page: "front", heading: "保管場所と保持者(手書き)",
      lines: [text("この紙の保管場所 1: ____________________  保持者(氏名)____________  受け取った日 ________"), text("この紙の保管場所 2: ____________________  保持者(氏名)____________  受け取った日 ________")] },
  ];
  return [...head, ...stepBlocks(f), ...tail];
}
