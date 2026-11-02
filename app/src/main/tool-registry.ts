// 道具の定義(src/tools/<道具名>.json。署名した同梱物)の読み込みと検査器。主プロセスの許可リストの実体。
// 許可リスト = ここに読み込んだ定義の名前の集合。エージェントのプロセスへ渡す定義もここから出す。
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { Ajv2020 } from "ajv/dist/2020.js";
import type { ValidateFunction } from "ajv/dist/2020.js";

export interface ToolDef {
  readonly name: string;
  readonly description: string;
  readonly input_schema: Readonly<Record<string, unknown>>;
}

export class ToolRegistry {
  private readonly validators = new Map<string, ValidateFunction>();
  private readonly defs = new Map<string, ToolDef>();

  constructor(defs: readonly ToolDef[]) {
    const ajv = new Ajv2020({ strict: false, allErrors: true });
    for (const d of defs) {
      this.defs.set(d.name, d);
      this.validators.set(d.name, ajv.compile(d.input_schema));
    }
  }

  /** 定義のディレクトリ(1 ファイル 1 道具)から読む。 */
  static fromDir(dir: string): ToolRegistry {
    const names = readdirSync(dir).filter((f) => f.endsWith(".json")).sort();
    return new ToolRegistry(names.map((f) => JSON.parse(readFileSync(join(dir, f), "utf8")) as ToolDef));
  }

  has(name: string): boolean {
    return this.defs.has(name);
  }

  names(): string[] {
    return [...this.defs.keys()];
  }

  /** エージェントのプロセスへ IPC で渡す定義の一覧。 */
  definitions(): ToolDef[] {
    return [...this.defs.values()];
  }

  /** 道具の入力の定義に、その名前の欄があるか(run_id を取る道具か、など)。 */
  hasProperty(name: string, prop: string): boolean {
    const props = this.defs.get(name)?.input_schema["properties"];
    return typeof props === "object" && props !== null && Object.hasOwn(props, prop);
  }

  /** 入力が定義の型に合うか。 */
  accepts(name: string, input: unknown): boolean {
    const v = this.validators.get(name);
    return v !== undefined && v(input) === true;
  }
}
