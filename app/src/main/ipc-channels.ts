// 画面のプロセス(レンダラ)と主プロセスの間の IPC(完了条件 1: 画面はエージェントのプロセスと直接通信しない)。
// レンダラが呼べるのは下の preload の窓口だけ。承認の署名鍵・キーチェーン・LLM API キーを返す口は無い。

import type { UiChatLine } from "./chat-view.ts";
import type { SurveyView } from "./survey-view.ts";
import type { TargetView } from "./target-actions.ts";

/** 5 面(完了条件 28)。最上位の画面はこれだけ。 */
export const SURFACES = ["chat", "approval-card", "secret-box", "phrase-panel", "status"] as const;
export type Surface = (typeof SURFACES)[number];

export const CHANNEL = {
  chatSend: "chat:send",
  chatAbort: "chat:abort",
  /** 最初の一歩の「始める」(spec 3.7。D-0169)。固定の指示を人の発話として送る。 */
  chatStart: "chat:start",
  cardDecision: "card:decision",
  secretSubmit: "secret:submit",
  phraseAction: "phrase:action",
  statusAction: "status:action",
  /** 人が接続先を登録・指紋を入力・削除する(エージェントの道具ではない)。 */
  targetRegister: "target:register",
  targetFingerprint: "target:fingerprint",
  targetRemove: "target:remove",
  /** 主プロセス → レンダラ: 面の表示内容(描画済みのモデル。秘密を含むのはフレーズのパネルだけ)。 */
  render: "ui:render",
} as const;

export type CardDecision = { readonly kind: "approve"; readonly proofs: Readonly<Record<string, string>> } | { readonly kind: "cancel" } | { readonly kind: "close" };
export type SecretMode = "save" | "app-open" | "no-save";
export type PhraseAction = "verify" | "print" | "confirm" | "handwritten" | "cancel";

export type UiMessage =
  | { readonly surface: "chat"; readonly lines: readonly UiChatLine[]; readonly sendLabel: string; readonly start: { readonly guide: string; readonly label: string } | null; readonly wait: string | null }
  | { readonly surface: "approval-card"; readonly card: unknown }
  | { readonly surface: "secret-box"; readonly mode: SecretMode; readonly ref: string; readonly label: string; readonly submitLabel: string }
  | { readonly surface: "phrase-panel"; readonly open: boolean; readonly words: readonly { readonly n: number; readonly word: string }[] | null; readonly canProceed: boolean; readonly actions: readonly { readonly action: PhraseAction; readonly label: string }[]; readonly notice: string | null; readonly cautions: readonly string[] }
  | { readonly surface: "status"; readonly heading: string; readonly rows: readonly { readonly label: string; readonly value: string }[]; readonly askAi: readonly { readonly kind: string; readonly label: string }[]; readonly target: TargetView; readonly survey: SurveyView | null; readonly steps: UiSteps | null };

/** 導入の進み具合の 1 行(手順の番号・短い見出し・合格か「次」か未着手か)。 */
/** 導入の進み具合の一覧(見出しと「次」の札の文は主プロセスが送る)。 */
export interface UiSteps { readonly heading: string; readonly nextLabel: string; readonly rows: readonly UiStep[] }
export interface UiStep { readonly n: number; readonly label: string; readonly state: "passed" | "next" | "todo" }
