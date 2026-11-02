// アプリが作る封筒(spec 3.1.1 の例外、3.7 のアプリの原因コードの表)。道具の拒否・SSH・封筒の失敗をモデルへ返す形。
// 核の原因コード(3.1.3)には載せない。カタログでは別の名前空間(app.*)で持つ。

export type AppErrorCode =
  | "TOOL_DENIED" | "TOOL_ARG_INVALID" | "PATH_REF_UNKNOWN" | "HUMAN_CANCELLED" | "QUOTE_REJECTED"
  | "SSH_UNREACHABLE" | "SSH_AUTH_DENIED" | "SSH_HOSTKEY_MISMATCH" | "SSH_DISCONNECTED"
  | "ENVELOPE_INVALID" | "ENVELOPE_SECRET_MISMATCH" | "ENVELOPE_TOO_MANY_RECORDS" | "ENVELOPE_EXIT_UNKNOWN"
  | "BOOTSTRAP_SIGNATURE_INVALID" | "BOOTSTRAP_PRIVILEGE_REQUIRED" | "BOOTSTRAP_PLATFORM_UNSUPPORTED" | "BOOTSTRAP_PACKAGE_FAILED"
  | "BOOTSTRAP_ALREADY_DONE" | "BOOTSTRAP_NOT_DONE" | "BOOTSTRAP_RESTORE_ORDER";

export interface AppNext {
  readonly argv: readonly string[];
  readonly requires_human: boolean;
  readonly requires_approval: boolean;
  readonly why: null;
}

export interface AppEnvelope {
  readonly schema: "sitate.out/v1";
  readonly command: readonly string[];
  readonly ok: false;
  readonly dry_run: false;
  readonly run_id: string | null;
  readonly result: null;
  readonly warnings: readonly [];
  readonly error: {
    readonly code: AppErrorCode;
    readonly class: "input" | "precondition" | "external" | "internal" | "verification";
    readonly retryable: boolean;
    readonly detail: Readonly<Record<string, unknown>>;
    readonly next: readonly AppNext[];
  };
}

interface Row {
  readonly class: AppEnvelope["error"]["class"];
  readonly retryable: boolean;
  readonly next: readonly AppNext[];
}

const human = (argv: readonly string[]): AppNext => ({ argv, requires_human: true, requires_approval: false, why: null });
const plain = (argv: readonly string[]): AppNext => ({ argv, requires_human: false, requires_approval: false, why: null });

/** 3.7 のアプリの原因コードの表のうち、封筒にする行(`error.class`・`retryable`・`next`)。 */
const ROWS: Readonly<Record<AppErrorCode, Row>> = {
  TOOL_DENIED: { class: "input", retryable: false, next: [] },
  TOOL_ARG_INVALID: { class: "input", retryable: false, next: [] },
  PATH_REF_UNKNOWN: { class: "input", retryable: false, next: [plain(["sitate", "source", "check"]), plain(["sitate", "source", "survey"])] },
  HUMAN_CANCELLED: { class: "precondition", retryable: false, next: [human([])] },
  QUOTE_REJECTED: { class: "input", retryable: false, next: [] },
  SSH_UNREACHABLE: { class: "external", retryable: true, next: [human([])] },
  SSH_AUTH_DENIED: { class: "precondition", retryable: false, next: [human([])] },
  SSH_HOSTKEY_MISMATCH: { class: "verification", retryable: false, next: [human([])] },
  SSH_DISCONNECTED: { class: "external", retryable: true, next: [plain(["sitate", "status"])] },
  ENVELOPE_INVALID: { class: "internal", retryable: false, next: [plain(["sitate", "version"]), plain(["sitate", "status"])] },
  ENVELOPE_SECRET_MISMATCH: { class: "internal", retryable: false, next: [plain(["sitate", "status"])] },
  ENVELOPE_TOO_MANY_RECORDS: { class: "internal", retryable: false, next: [plain(["sitate", "version"]), plain(["sitate", "status"])] },
  ENVELOPE_EXIT_UNKNOWN: { class: "internal", retryable: false, next: [plain(["sitate", "version"]), plain(["sitate", "status"])] },
  BOOTSTRAP_SIGNATURE_INVALID: { class: "verification", retryable: false, next: [human([])] },
  BOOTSTRAP_PRIVILEGE_REQUIRED: { class: "precondition", retryable: false, next: [human([])] },
  BOOTSTRAP_PLATFORM_UNSUPPORTED: { class: "precondition", retryable: false, next: [human([])] },
  BOOTSTRAP_PACKAGE_FAILED: { class: "external", retryable: true, next: [plain(["bootstrap"])] },
  BOOTSTRAP_ALREADY_DONE: { class: "precondition", retryable: false, next: [human(["bootstrap", "--rekey"])] },
  BOOTSTRAP_NOT_DONE: { class: "precondition", retryable: false, next: [plain(["bootstrap"])] },
  BOOTSTRAP_RESTORE_ORDER: { class: "input", retryable: false, next: [] },
};

export interface EnvelopeContext {
  /** 最初の `bootstrap`(rekey でない)か `bootstrap-restore` の `place`。SSH_AUTH_DENIED の次の一手が変わる(資格情報を入れ直す)。 */
  readonly firstBootstrap?: boolean;
}

/** 理由・文脈で変わる次の一手(spec 3.7 のアプリの原因コードの表)。 */
function nextFor(code: AppErrorCode, command: readonly string[], row: Row, ctx: EnvelopeContext): readonly AppNext[] {
  // 到達できない: 人が確かめた後に同じ道具を呼び直す(導入の後は sitate status)。
  if (code === "SSH_UNREACHABLE") return [human([]), plain(command[0] === "bootstrap" || command[0] === "bootstrap-restore" ? command : ["sitate", "status"])];
  // 認証が拒否された: 最初の bootstrap は資格情報を入れ直して同じ道具を呼び直す。管理用 SSH は事業者のコンソールで開き直した管理アクセスで bootstrap --rekey。
  if (code === "SSH_AUTH_DENIED") return ctx.firstBootstrap === true ? [human([]), plain(command)] : [human(["bootstrap", "--rekey"])];
  return row.next;
}

/** アプリの原因コードの封筒を作る。detail は列挙された欄だけを呼び出し側が渡す(秘密・パスの実値を入れない)。 */
export function appErrorEnvelope(code: AppErrorCode, command: readonly string[], detail: Readonly<Record<string, unknown>> = {}, runId: string | null = null, ctx: EnvelopeContext = {}): AppEnvelope {
  const row = ROWS[code];
  return { schema: "sitate.out/v1", command, ok: false, dry_run: false, run_id: runId, result: null, warnings: [], error: { code, class: row.class, retryable: row.retryable, detail, next: nextFor(code, command, row, ctx) } };
}
