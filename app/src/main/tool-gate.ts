// 主プロセスの道具の呼び出しの検査(spec 3.7「道具と実行」、AgentRuntime の補足)。境界の向こうから来た道具名と引数を信用せず、
// 許可リストと型を検査し、path_ref を元の値に戻して、核を呼ぶ操作(サブコマンドと引数と標準入力の材料)を作る。
// 秘密の値はここに入らない: secret_refs は参照名のまま計画に載せ、値の差し込みは呼び出し側(キーチェーンを持つ層)が buildStdin で行う。
import { REF_NAME_PATTERN } from "../constants.ts";
import { appErrorEnvelope } from "./app-envelope.ts";
import type { AppEnvelope } from "./app-envelope.ts";
import { PathRefTable, PathRefUnknownError } from "./path-ref.ts";
import { checkKindArgs } from "./tool-kind-args.ts";
import type { ToolRegistry } from "./tool-registry.ts";

export interface ToolCall {
  readonly id: string;
  readonly name: string;
  readonly input: unknown;
}

/** 核を呼ぶ操作。argv は `sitate` の後ろの引数。 */
export interface ToolPlan {
  readonly tool: string;
  readonly subcommand: readonly string[];
  readonly argv: readonly string[];
  readonly secretRefs: readonly string[];
  /** `--file -` の標準入力に渡す定義(path_ref を元の値に戻したもの)。 */
  readonly definition: Readonly<Record<string, unknown>> | null;
  readonly dryRun: boolean;
  readonly runId: string | null;
  /** path_ref を元の値に戻した入力(bootstrap などアプリが実行する道具が読む)。 */
  readonly input: Readonly<Record<string, unknown>>;
}

export type Authorization =
  | { readonly ok: true; readonly plan: ToolPlan }
  | { readonly ok: false; readonly envelope: AppEnvelope };

/** bootstrap・bootstrap-restore は核のサブコマンドではなく、アプリが SSH で実行する(アプリが封筒を作る)。 */
const APP_RUN_TOOLS = ["sitate_bootstrap", "sitate_bootstrap_restore"];
const SPECIAL_ARGS = ["run_id", "dry_run", "secret_refs", "definition", "kind"];

const deny = (code: "TOOL_DENIED" | "TOOL_ARG_INVALID" | "PATH_REF_UNKNOWN", command: readonly string[]): Authorization => ({ ok: false, envelope: appErrorEnvelope(code, command) });

export function authorizeToolCall(registry: ToolRegistry, call: ToolCall, pathRefs: PathRefTable): Authorization {
  const subcommand = subcommandOf(call.name);
  const command = ["app", "tool"];
  if (typeof call.name !== "string" || !registry.has(call.name)) return deny("TOOL_DENIED", command);
  if (!registry.accepts(call.name, call.input)) return deny("TOOL_ARG_INVALID", subcommand);
  const input = call.input as Record<string, unknown>;
  if (!refNamesOk(input)) return deny("TOOL_ARG_INVALID", subcommand);
  if (call.name === "sitate_propose" && !kindArgsOk(input)) return deny("TOOL_ARG_INVALID", subcommand);
  let restored: Record<string, unknown>;
  try {
    restored = pathRefs.restoreDeep(input);
  } catch (e) {
    if (e instanceof PathRefUnknownError) return deny("PATH_REF_UNKNOWN", subcommand);
    throw e;
  }
  return { ok: true, plan: planOf(call.name, subcommand, restored) };
}

const REF_NAME_RE = new RegExp(REF_NAME_PATTERN);

/** 入力のどの深さにある参照名(secret_refs の要素と *_ref の値。path_ref は別の表で扱う)も、字種の検査を通す。道具の定義の pattern とは独立の二重の守り。 */
export function refNamesOk(v: unknown): boolean {
  if (Array.isArray(v)) return v.every(refNamesOk);
  if (!isRecord(v)) return true;
  return Object.entries(v).every(([k, x]) => {
    if (k === "secret_refs") return Array.isArray(x) && x.every((r) => typeof r === "string" && REF_NAME_RE.test(r));
    if (k.endsWith("_ref") && k !== "path_ref") return typeof x === "string" && REF_NAME_RE.test(x);
    return refNamesOk(x);
  });
}

function kindArgsOk(input: Record<string, unknown>): boolean {
  const { kind, ...rest } = input;
  return typeof kind === "string" && checkKindArgs(kind, rest).ok;
}

/** 道具名 sitate_def_validate → ["def","validate"] ではなく、語の区切りが曖昧な名前は定義の名前から引く(下の SUBCOMMANDS)。 */
export function subcommandOf(name: unknown): string[] {
  return typeof name === "string" && Object.hasOwn(SUBCOMMANDS, name) ? [...SUBCOMMANDS[name]!] : ["app", "tool"];
}

/** 道具名と核のサブコマンドの語の対応(公開区分 tool の行。語の中の - は _ になっているので、名前から機械的に戻せないものがあるため表で持つ)。 */
const SUBCOMMANDS: Readonly<Record<string, readonly string[]>> = Object.fromEntries(
  [
    ["version"], ["doctor"], ["def", "validate"], ["def", "show"], ["init-storage"], ["bucket", "check"], ["init"], ["kit"],
    ["source", "enroll"], ["source", "check"], ["source", "survey"], ["restore-dest", "enroll"], ["restore-dest", "check"],
    ["notify", "set"], ["notify", "test"], ["backup"], ["verify"], ["extend"], ["prune"], ["hold", "set"], ["list"], ["restore"],
    ["restore-test"], ["status"], ["report"], ["estimate"], ["propose"], ["pending"], ["apply"], ["cancel"], ["attest"],
    ["bootstrap"], ["bootstrap-restore"],
  ].map((words) => ["sitate_" + words.join("_").replace(/-/g, "_"), words] as const),
);

function planOf(tool: string, subcommand: string[], input: Record<string, unknown>): ToolPlan {
  const argv: string[] = [...subcommand];
  if (typeof input.kind === "string") argv.push(input.kind);
  const secretRefs = Array.isArray(input.secret_refs) ? (input.secret_refs as string[]) : [];
  const runId = typeof input.run_id === "string" ? input.run_id : null;
  const dryRun = input.dry_run === true;
  const definition = isRecord(input.definition) ? input.definition : null;
  if (!APP_RUN_TOOLS.includes(tool)) {
    for (const [k, v] of Object.entries(input)) {
      if (SPECIAL_ARGS.includes(k) || v === undefined) continue;
      argv.push(...flagArgs(k, v));
    }
    if (definition !== null) argv.push("--file", "-");
    if (secretRefs.length > 0) argv.push("--secrets-stdin");
    if (dryRun) argv.push("--dry-run");
    if (runId !== null) argv.push("--run-id", runId);
  }
  return { tool, subcommand, argv, secretRefs, definition, dryRun, runId, input };
}

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

/** 引数 foo_bar の値を `--foo-bar <値>` にする。真偽値は印だけ(false は出さない)、配列・オブジェクトは JSON の 1 語、段の enum の値はそのまま。 */
function flagArgs(key: string, value: unknown): string[] {
  const flag = "--" + key.replace(/_/g, "-");
  if (typeof value === "boolean") return value ? [flag] : [];
  if (typeof value === "string") return [flag, value];
  if (typeof value === "number") return [flag, String(value)];
  return [flag, JSON.stringify(value)];
}

/**
 * 標準入力を組む。--secrets-stdin のときは 1 行目に `{"<参照名>": <値>}`、続けて --file - の定義(JSON の 1 行)。
 * 値は呼び出し側がキーチェーンから取り出して渡す。ここで保持・記録しない。
 */
export function buildStdin(plan: ToolPlan, secretValues: Readonly<Record<string, unknown>>): string {
  const lines: string[] = [];
  if (plan.secretRefs.length > 0) lines.push(JSON.stringify(Object.fromEntries(plan.secretRefs.map((r) => [r, secretValues[r]]))));
  if (plan.definition !== null) lines.push(JSON.stringify(plan.definition));
  return lines.length > 0 ? lines.join("\n") + "\n" : "";
}
