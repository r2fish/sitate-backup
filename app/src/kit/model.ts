// 復旧キットの描画部の内部の型(型のファイル types.ts は変えない)。
import type { KitBlockKind, KitPage, KitStepId } from "./types.ts";

/** 1 行(折り返す前の文)。mono は等幅(ハッシュ・コマンド)。 */
export interface Line {
  readonly t: string;
  readonly mono?: boolean;
  readonly bold?: boolean;
  /** コマンドの切れ目(この単位の途中では折らない)。無ければ空白で区切る。 */
  readonly units?: readonly string[];
}

/** 欄の中身。grid があるときは lines の下に等間隔の升目を置く(フレーズの記入欄)。 */
export interface BlockSpec {
  readonly kind: KitBlockKind;
  readonly page: KitPage;
  readonly step_id?: KitStepId;
  readonly heading: string;
  readonly lines: readonly Line[];
  readonly grid?: readonly string[];
}
