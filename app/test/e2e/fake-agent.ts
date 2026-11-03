// LLM の代役(偽のエージェント): 手順書の順で道具の呼び出し(tool_use)を返し、手順書の人への問い Q1〜Q4 と Q2 を、説明と推奨の案を添えた問いとして出す。
// 流れる先は本物: アプリの主プロセス、utilityProcess のエージェント、Tool Runner、核、受入試験の環境(作業指示書 5.14 の追加の完了条件)。
// 状態を持たない: 会話の履歴(要求の本文)から「何回目の人の入力の後の、何番目の道具の呼び出しか」を数えて次の応答を決める。
import type { FakeResponder, FakeStep } from "./llm-endpoint.ts";
import { HOOK_NAME } from "./script.ts";
import type { Answers } from "./script.ts";
import { candidatesFrom, sourceTargets, surveyFrom } from "./fake-definition.ts";
import type { Candidate, Survey } from "./fake-definition.ts";

interface PlanStep { readonly hold?: () => Promise<void>; readonly make: (prevResult: string) => FakeStep }
const call = (name: string, input: Record<string, unknown> = {}): PlanStep => ({ make: () => ({ toolUses: [{ name, input }] }) });
const say = (text: string): PlanStep => ({ make: () => ({ text }) });

const LOCATION_Q_TEXT = "導入を始めます。管理アカウントの秘密鍵は、秘密の入力ボックスから入れていただきます。まず、バックアップサーバーと元サーバーの所在と、それぞれの管理アカウントを教えていただけますか?";
const Q1_TEXT = "次に保管先を決めます。保管先のエンドポイントとバケット名、通知先を教えてください。何のために聞くか: バックアップの送り先と、失敗したときの知らせ先を決めるためです。推奨の案: 契約した事業者の値をそのまま使い、通知先は普段見る場所の受信 Webhook を 1 つにします。分からなければ推奨の案でよいです。教えていただけますか?";
const Q3_TEXT = "次に復元先を決めます。復元先のホストを教えてください。何のために聞くか: 毎月の復元テストを別のサーバーで本当に試すためです。推奨の案: 本番と別のネットワークの小さなサーバーを 1 台使います。分からなければ推奨の案でよいです。教えていただけますか?";
const Q4_TEXT = "元サーバーを調べました。何のために聞くか: 取れていない場所は被災のときに戻せないためです。推奨の案: 調査で見つけた取れる候補をすべて含めます。分からなければ推奨の案でよいです。何を守りたいですか?";
const Q4_PROPOSAL = "いただいた希望に当たる場所を対象にした案を作りました。取れない場所はその理由とともに状態の表示にあります。この案で進めてよろしいでしょうか?";
const Q2_TEXT = "復旧のフレーズを控えたことを確かめます。何のために聞くか: バックアップサーバーを全部失ったときの唯一の手掛かりだからです。推奨の案: 別々の場所に 2 部置きます。分からなければ推奨の案でよいです。印刷か手書きで 2 か所に保管しましたか?";

function definitionOf(a: Answers, survey?: Survey, added: readonly Candidate[] = []): Record<string, unknown> {
  return {
    version: 1,
    source: { host: a.sourceHost, ...sourceTargets(survey, added) },
    storage: [{ endpoint: a.storageEndpoint, bucket: a.bucket, key_ref: "storage", prefix: "sitate/" }],
    restore: { adapter: "ssh-host", dest: { at: `ssh://${a.adminUser}@${a.destHost}:${a.destPort}`, key_ref: "restore-init" } },
    notify: { to: [HOOK_NAME] },
  };
}

/** 台本(人の入力ごとの道具の呼び出しの列)。`hold` は、その呼び出しの前に待つ処理(保管先準備アダプタの代役を置く)。 */
export interface PlanOptions {
  /** 保管先準備アダプタ(既定 minio。実物の B2 の予行は b2)。 */
  readonly adapter?: string;
  /** 予行: backup の後に extend と bucket check を足す(保持の延長と、保持の短縮の試しの結果 permissions[] を記録に出す)。 */
  readonly extras?: boolean;
}

export function planOf(a: Answers, afterBootstrap: () => Promise<void>, o: PlanOptions = {}, survey?: Survey, added: readonly Candidate[] = []): PlanStep[][] {
  const s3 = { endpoint: a.storageEndpoint, bucket: a.bucket, prefix: "sitate/", adapter: o.adapter ?? "minio", admin_ref: "admin" };
  const sourceHost = `ssh://${a.adminUser}@${a.sourceHost}`;
  const destAt = `ssh://${a.adminUser}@${a.destHost}:${a.destPort}`;
  const definition = definitionOf(a, survey, added);
  // 手順 8(3.8): 足せる候補(mount・db)があれば、人に問わず定義へ足し、def validate → source enroll 再実行 → 完了確認のやり直し。seal の前なので承認は要らない。
  const readdSources = added.length > 0 ? [call("sitate_def_validate", { definition }), call("sitate_source_enroll", { host: sourceHost, admin_ref: "source-admin" }), call("sitate_source_check")] : [];
  return [
    [say(LOCATION_Q_TEXT)], // 「始める」の固定の指示への応答。所在と管理アカウントは開始時の入力で、問いに数えない(spec 6.5 の A2)
    [call("sitate_bootstrap", { host: a.backupHost, admin_user: a.adminUser, admin_ref: "bak-admin" }), { hold: afterBootstrap, make: () => ({ toolUses: [{ name: "sitate_version", input: {} }] }) },
      call("sitate_doctor"), say(Q1_TEXT)],
    [call("sitate_init_storage", { ...s3, dry_run: true }), say(Q3_TEXT)],
    [call("sitate_restore_dest_enroll", { dest: destAt, adapter: "ssh-host", admin_ref: "dest-admin", dry_run: true }), call("sitate_source_survey", { host: sourceHost, admin_ref: "source-admin" }), say(Q4_TEXT)],
    [call("sitate_notify_set", { to: HOOK_NAME, secret_ref: HOOK_NAME }), call("sitate_def_validate", { definition }), call("sitate_notify_test"), say(Q4_PROPOSAL)],
    [call("sitate_init_storage", s3), call("sitate_init"), call("sitate_bucket_check"), call("sitate_doctor"), say(Q2_TEXT)],
    [call("sitate_source_enroll", { host: sourceHost, admin_ref: "source-admin" }), call("sitate_source_check"), ...readdSources,
      call("sitate_restore_dest_enroll", { dest: destAt, adapter: "ssh-host", admin_ref: "dest-admin" }), call("sitate_restore_dest_check"),
      call("sitate_notify_test"), call("sitate_propose", { kind: "seal", definition }),
      call("sitate_apply"), // 承認の結果の封筒に変更の識別子は載らない(propose の中で承認まで済む)ので、id を渡さず最後に承認した変更を適用する。
      call("sitate_def_show"), call("sitate_status"), call("sitate_backup"), call("sitate_restore_test"), ...(o.extras ? [call("sitate_extend"), call("sitate_bucket_check")] : []), call("sitate_status"), call("sitate_doctor"),
      say("導入が完了しました。")],
  ];
}

const isToolResult = (m: unknown): boolean => {
  const c = (m as { content?: unknown }).content;
  return Array.isArray(c) && c.some((b) => (b as { type?: string }).type === "tool_result");
};

export function makeFakeAgent(a: Answers, afterBootstrap: () => Promise<void>, fixedText?: string, o: PlanOptions = {}): FakeResponder {
  return async (body) => {
    const msgs = body.messages as { role: string }[];
    const plan = planOf(a, afterBootstrap, o, surveyFrom(msgs), candidatesFrom(msgs)); // 定義の対象は履歴の調査の結果から決める(状態を持たない)
    if (fixedText !== undefined) return { text: fixedText };
    const humanIdx = msgs.flatMap((m, i) => (m.role === "user" && !isToolResult(m) ? [i] : []));
    const turn = humanIdx.length - 1;
    const step = (msgs.length - 1 - (humanIdx.at(-1) ?? 0)) / 2;
    const entry = plan[turn]?.[step];
    if (entry === undefined) return { text: "ご依頼のとおり進めました。" };
    await entry.hold?.();
    return entry.make("");
  };
}
