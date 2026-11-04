// アプリが作る封筒をモデルへ渡す区画にする(spec 3.7「アプリが作る封筒」。D-0199)。核の封筒と同じ shapeEnvelope → toDataSection を通す、アプリの封筒の唯一の口。
// 封筒を作って返すだけで、その会話の核の結果の記憶(引用の照合・手順の判定)には触れない。
import { appErrorEnvelope } from "./app-envelope.ts";
import type { AppEnvelope, AppErrorCode, EnvelopeContext } from "./app-envelope.ts";
import type { PathRefTable } from "./path-ref.ts";
import { shapeEnvelope, toDataSection } from "./shape-envelope.ts";

/** 作った封筒(`command` は道具に当たるサブコマンドの語。当たらなければ ["app","tool"])を整形して区画にする。 */
export function appSection(env: AppEnvelope, refs: PathRefTable): string {
  return toDataSection(shapeEnvelope(env as unknown as Record<string, unknown>, refs));
}

/** アプリの原因コードの封筒を作って区画にする。 */
export function appErrorSection(code: AppErrorCode, command: readonly string[], refs: PathRefTable, detail: Readonly<Record<string, unknown>> = {}, ctx: EnvelopeContext = {}): string {
  return appSection(appErrorEnvelope(code, command, detail, null, ctx), refs);
}
