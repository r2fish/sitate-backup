// 復元の手順 (1)〜(6) の欄(spec 4.7 の字面)。コマンドは `fields.steps` の argv があれば使い、無い手順(2-no-app・3-no-adapter・4-journal など)は
// `fields` の値から字面で組み立てる。場合分け(アプリが無いとき、保管先準備アダプタの無い事業者、など)は `fields.steps` に関わらず必ず刷る。
import type { BlockSpec, Line } from "./model.ts";
import type { KitFields, KitStepId } from "./types.ts";
import { argvLine, cmdLine, plainCmd } from "./cmd.ts";

const text = (t: string): Line => ({ t });
const RUN_GEN = ["RUN=r-$(date -u +%Y%m%dT%H%M%SZ)-$(head -c 2 /dev/urandom", "|", "od -An -tx1", "|", "tr -d ' \\n')"];
const RESTORE_DIR = "/var/lib/sitate/restore/$RUN/to";
const SUDO = ["sudo", "-u", "sitate", "sitate"];

const HEADINGS: Record<KitStepId, string> = {
  "1": "復元の手順 (1)",
  "2": "復元の手順 (2)",
  "2-no-app": "復元の手順 (2) アプリが無いとき",
  "3": "復元の手順 (3)",
  "3-no-adapter": "復元の手順 (3) 保管先準備アダプタの無い事業者と、アプリも配布元も無いとき",
  "4-journal": "復元の手順 (4) の記録",
  "4": "復元の手順 (4)",
  "5": "復元の手順 (5)",
  "6": "復元の手順 (6)",
};

export const STEP_ORDER: readonly KitStepId[] = ["1", "2", "2-no-app", "3", "3-no-adapter", "4-journal", "4", "5", "6"];

function conn(f: KitFields): string[] {
  const s = f.storage;
  return ["--endpoint", s.endpoint, "--bucket", s.bucket, "--prefix", s.prefix];
}

function keyed(f: KitFields): string[] {
  const s = f.storage;
  return ["--keyring", s.keyring_id, "--key-ref", s.key_ref, "--repo-id", s.repo_id, "--phrase-stdin"];
}

/** その手順の argv。核が出していればそれ、無ければ fields の値から組み立てる。 */
function argvOf(f: KitFields, id: KitStepId): string[] {
  const given = f.steps.find((s) => s.id === id)?.argv;
  if (given !== undefined && given.length > 0) return [...given];
  if (id === "3-no-adapter") return [...SUDO, "secrets", "set", f.storage.key_ref, "--role", "read", "--stdin"];
  if (id === "4-journal") return [...SUDO, "journal", "verify", ...conn(f), ...keyed(f)];
  return [];
}

/** (5) の argv。`--to` が無ければ足す。 */
function restoreArgv(f: KitFields): string[] {
  const a = argvOf(f, "5");
  return a.length === 0 || a.includes("--to") ? a : [...a, "--to", RESTORE_DIR];
}

function toDir(f: KitFields): string {
  const a = restoreArgv(f);
  const i = a.indexOf("--to");
  return i >= 0 && a[i + 1] !== undefined ? a[i + 1] : RESTORE_DIR;
}

const SECRET_INPUT: Line[] = [
  text("秘密の標準入力(3〜6): 標準入力の 1 行目(事業者の管理用鍵の JSON、鍵の JSON、フレーズの 24 語)は、先に次の read -rs X でシェル変数に読み(入力を画面に出さない)、printf で渡す。値をコマンド行や echo に書かず、シェルの履歴とディスクに残さない。使い終えたら unset X で消す。"),
  plainCmd(["read", "-rs", "X"]),
  cmdLine(["printf '%s\\n'", '"$X"', "|", "sudo -u sitate sitate", "…"]),
  plainCmd(["unset", "X"]),
];

function linesFor(f: KitFields, id: KitStepId): Line[] {
  const argv = argvOf(f, id);
  switch (id) {
    case "1":
      return [text("(1) 新しいホストを用意する。対応は Linux の x86_64 と arm64 で、root の権限を持つ初期資格情報で入れること((2) の置き先に書くため)。")];
    case "2":
      return [
        text("(2) 核と保管先準備アダプタ(ある事業者だけ)を得て、上の「配布者の公開鍵」の欄のコマンドで確かめ、置き先(核は /usr/local/bin/sitate、アダプタは /usr/libexec/sitate/adapters/provision/<name>/)に置く。得る先: アプリがあれば bootstrap-restore の place の段がアプリの同梱の bundle/<version>/ から置く。アプリが無ければ配布元の取得先 URL から、版のアーカイブ 1 つ(bundle-<version>.tar.gz。中は bundle/<version>/ からの相対パスのまま)とその .sig を落とし、署名を確かめて展開してから、保管先の bin/ と同じ照合を行う。アプリも配布元も無いときは、先に事業者のコンソールで読み取り鍵を発行し((3) で使う)、コンソールで保管先の bin/ の置き場から落とす(裏面)。人が確かめるのは核だけ(アダプタは置いた核が起動前に検証する)。"),
        text("版の選び方: 保管先の bin/ の同じキーに版が複数あるときは、SHA-256 がこの紙と合う版を使う。この紙の核の版が無いか合う版が無いときは、保管先の bin/ の版のうち署名がこの紙の公開鍵で合う最も新しい版を使う。配布元から落とした版がこの紙の核の版と違うときも署名で確かめる。"),
        text("署名での確かめ方: minisign -V が表示する trusted comment の version・arch・path が落としたもの(版ディレクトリの <version>、<os>-<arch>、版ディレクトリからの相対パス。例 linux-x86_64/sitate)と合うことも確かめ、合わなければその版を使わない(署名済みの旧版を別の版のキーに積むすり替えの対策)。"),
      ];
    case "2-no-app":
      return [
        text("(2) アプリが無いとき: 置いた後に、次の 2 つのコマンドで専用ユーザーを作り、状態ディレクトリを作る。(3) 以降のコマンドは、初期資格情報のユーザーから sudo -u sitate sitate … で専用ユーザー sitate として実行する。root でログインし sudo の無いホストでは、(3) 以降の sudo -u sitate を runuser -u sitate -- に読み替える。アプリがあれば bootstrap-restore の place の段が専用ユーザーを作る。"),
        plainCmd(["useradd", "--system", "sitate"]),
        plainCmd(["install", "-d", "-o", "sitate", "-g", "sitate", "-m", "0700", "/var/lib/sitate"]),
      ];
    case "3":
      return [
        text("(3) 読み取り鍵を発行する(保管先の契約者が持つ事業者の管理用鍵から)。先に、次の run_id を (3) の前に一度だけ作る。(3) をやり直すときも同じ \"$RUN\" を付ける(同じ --run-id なら再発行しないため、やり直しで読み取り鍵が増えない)。--run-id \"$RUN\" は (3) にだけ付ける(ほかの手順の --run-id には付けない)。"),
        cmdLine(RUN_GEN),
        text("標準入力の 1 行目に、事業者の管理用鍵を次の形で渡す。発行した鍵は参照名 key_ref(上の「保管先」の欄)の役割 read で保存され、製品の印が付く。保管先の repo_id がこの紙と違えば、核は発行した鍵を直ちに失効させてから REPO_ID_MISMATCH で止める(別のリポジトリ)。"),
        cmdLine(['{"admin": <事業者の管理用鍵の値>}']),
        argvLine(argv),
        ...SECRET_INPUT,
      ];
    case "3-no-adapter":
      return [
        text("(3) 保管先準備アダプタの無い事業者と、アプリも配布元も無いとき: storage issue-read-key は飛ばし、事業者のコンソールで発行した鍵(アダプタの無い事業者は事業者別付録、アプリも配布元も無いときは (2))を、次のコマンドで参照名 key_ref(上の「保管先」の欄)の役割 read に入れる。標準入力の 1 行目に次の形で鍵を渡す。コンソールで付ける鍵の名前は製品の印(裏面)。"),
        cmdLine(['{"key_id": "<鍵の ID>", "secret": "<鍵の秘密>"}']),
        argvLine(argv),
        text("アプリも配布元も無いとき(保管先準備アダプタのある事業者)は、運用を再開するときの R3 の init --adopt がこの鍵も旧鍵として失効させる。保管先準備アダプタの無い事業者では init --adopt は鍵を失効させないため、(6) で人がコンソールで失効させる。"),
      ];
    case "4-journal":
      return [
        text("(4) の記録: 世代を選ぶ前に(次の list より先に)、保管先の記録(journal/)の連鎖を確かめ、項目の一覧(連番・保管先の書き込み時刻・種別・run_id・結果)で経緯を見る。標準入力の 1 行目にフレーズの 24 語を渡す。連鎖に切れがあれば警告 JOURNAL_CHAIN_BROKEN が出る(detail は最初の切れ)。"),
        argvLine(argv),
      ];
    case "4":
      return [
        text("(4) 日付・保管先の書き込み時刻(stored_at)・健常性・全対象 ok か・復元テストで確認済みかを見て世代を選ぶ(標準入力の 1 行目にフレーズの 24 語)。侵害が疑われるときは、侵害が始まった日より前の stored_at の世代を選ぶ。選んだ世代は、運用を再開するときの R3 でも定義の写しを読む世代として使う。"),
        argvLine(argv),
        text("錨: 管理者端末のアプリが残っていれば、アプリが保存した最後の合格記録を --anchor <ファイル> で渡す。anchor.rollback_suspected の印が付いたら、保管先が合格記録より前へ巻き戻された疑いがあるため、侵害を前提に stored_at で世代を選ぶ。anchor.expired(合格した世代が保持期限を過ぎて正当に消えた)は巻き戻しの印ではない。錨が見つけるのは端末が最後に受理した時点より前への巻き戻しだけで、それより後に積まれた偽の世代は見つけない。"),
      ];
    case "5":
      return [
        text("(5) 選んだ世代(--backup。通常は latest)を空のディレクトリへ復元する。空のディレクトリは先に次のコマンドで作る($RUN は (3) の値)。ファイルの書き出しとダンプの配置は核が行う。結果の確かめ方は下の「復元できた証明」の欄。"),
        argvLine([...SUDO.slice(0, 3), "install", "-d", "-m", "0700", toDir(f)]),
        argvLine(restoreArgv(f)),
      ];
    default:
      return [
        text("(6) 復元が終わったら (3) の読み取り鍵を失効させる(標準入力の 1 行目は (3) と同じ)。アプリも配布元も無く、事業者の管理用鍵も手元に無いときは、(2) の鍵をコンソールで失効させる(裏面)。保管先準備アダプタの無い事業者は、(3) で入れた鍵をコンソールで失効させる(事業者別付録。裏面)。"),
        argvLine(argv),
        text("運用を続けるときは、手順書の付録「全損からの復元」の R2 以降へ進む(init --adopt には key_ref を --key-ref で付ける)。保管先準備アダプタの無い事業者は、R3 の前に事業者のコンソールで書き込み鍵と読み取り鍵を発行して secrets set <key_ref> --role write --stdin と --role read --stdin で入れ、init --adopt は --admin-ref なしで実行する。"),
      ];
  }
}

/** 見出しが手順の番号と場合分けを持つので、最初の文の頭の「(n) …: 」は刷らない。 */
function dropLead(id: KitStepId, lines: Line[]): Line[] {
  const [first, ...rest] = lines;
  const variant = id === "2-no-app" || id === "3-no-adapter" || id === "4-journal";
  return [{ ...first, t: first.t.replace(variant ? /^\(\d\) [^:]*: / : /^\(\d\) /, "") }, ...rest];
}

export function stepBlocks(f: KitFields): BlockSpec[] {
  return STEP_ORDER.map((id) => ({ kind: "restore_step", page: "front", step_id: id, heading: HEADINGS[id], lines: dropLead(id, linesFor(f, id)) }));
}
