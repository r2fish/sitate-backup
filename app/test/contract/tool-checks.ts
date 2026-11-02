import { Ajv2020 } from "ajv/dist/2020.js";

// 道具の定義(src/tools/<道具名>.json)の検査の関数群。定義の書き方(enum か pattern か oneOf か)に依らず、Ajv で実際に値を通して照らす。
// 定義の形: { name, description, input_schema }(src/tools/README.md)。tools.test.ts が実物と、自己検査用の見本に使う。
export type ToolDef = { name: string; description: string; input_schema: any };
export type GateFlag = { name: string; value: string; enum?: string[] };

const snake = (s: string) => s.replace(/-/g, "_");

/**
 * gate の value が json の引数の、JSON の値の型(spec 1218 行 字句規則 5)。gate の値の種類 json だけでは 3 種を分けられないので試験が持つ。
 * 配列: --paths、--providers、--allow-from。文字列: attest --note、--authorized-key。そのほかの json はオブジェクト(--source、--restore、--notify、--with-restore-change)。
 */
export const JSON_ARRAY_FLAGS = ["paths", "providers", "allow-from"];
export const JSON_STRING_FLAGS = ["note", "authorized-key"];
export function jsonShape(flag: string): "array" | "string" | "object" {
  return JSON_ARRAY_FLAGS.includes(flag) ? "array" : JSON_STRING_FLAGS.includes(flag) ? "string" : "object";
}

/** 引数を渡して通るかを検査する関数。errors は instancePath つきの Ajv の結果。 */
function compile(schema: any) {
  return new Ajv2020({ strict: false, allErrors: true }).compile(schema);
}

/** 値 v が schema を通るか。 */
export function accepts(schema: any, v: unknown): boolean {
  return compile(schema)(v) === true;
}

/** 定義の引数名の集合。oneOf・anyOf の枝の引数を含む。 */
export function argNames(input: any): Set<string> {
  const out = new Set<string>(Object.keys(input.properties ?? {}));
  for (const v of [...(input.oneOf ?? []), ...(input.anyOf ?? [])]) for (const k of Object.keys(v.properties ?? {})) out.add(k);
  return out;
}

/** 引数 prop を定義している部分スキーマ(最上位と枝)。 */
export function propSchemas(input: any, prop: string): any[] {
  const branches = [...(input.oneOf ?? []), ...(input.anyOf ?? [])];
  return [input.properties?.[prop], ...branches.map((b: any) => b.properties?.[prop])].filter((x) => x !== undefined);
}

/** 包み方({name, description, input_schema})の検査。 */
export function shapeProblems(file: string, def: any): string[] {
  const out: string[] = [];
  if (def?.name !== file) out.push(`${file}: name が道具名と違う`);
  if (typeof def?.description !== "string" || def.description === "") out.push(`${file}: description が無い`);
  if (def?.input_schema?.type !== "object") out.push(`${file}: input_schema の type が object でない`);
  return out;
}

/** gate の value の種別から導く型の検査(値を実際に通す)。 */
export function typeProblems(tool: string, input: any, flags: GateFlag[], extra: string[]): string[] {
  const out: string[] = [];
  const bad = (prop: string, why: string) => out.push(`${tool}.${prop}: ${why}`);
  const probe = (f: GateFlag): { reject: unknown[]; acceptAll?: unknown[]; acceptSome?: unknown[] } | null => {
    switch (f.value) {
      case "none": return { reject: ["x", 1], acceptAll: [true, false] };
      case "json": {
        const shape = jsonShape(f.name);
        if (shape === "string") return { reject: [1, true, {}, [], ["x"]], acceptSome: ["x", "説明", "ssh-ed25519 AAAA"] };
        if (shape === "array") return { reject: ["x", 1, true, {}], acceptSome: [[], ["x"], ["pr:0123456789abcdef"]] };
        return { reject: ["x", 1, true, []], acceptSome: [{}, { a: 1 }] };
      }
      case "int": return { reject: ["x", true], acceptSome: [1, 24, 90] };
      case "enum": return { reject: ["__no_such_value__", 1], acceptAll: f.enum ?? [] };
      case "word": case "ref": case "base64": return { reject: [1, true, {}] };
      default: return null;
    }
  };
  const all: GateFlag[] = [...flags, ...extra.map((n) => ({ name: n, value: "none" }))];
  for (const f of all) {
    const p = probe(f);
    const prop = snake(f.name);
    const schemas = propSchemas(input, prop);
    if (!p || schemas.length === 0) continue;
    for (const s of schemas) {
      for (const v of p.reject) if (accepts(s, v)) bad(prop, `${f.value} の引数が ${JSON.stringify(v)} を受ける`);
      for (const v of p.acceptAll ?? []) if (!accepts(s, v)) bad(prop, `${f.value} の引数が ${JSON.stringify(v)} を受けない`);
      if (p.acceptSome && !p.acceptSome.some((v) => accepts(s, v))) bad(prop, `${f.value} の引数が正しい値をどれも受けない(${JSON.stringify(p.acceptSome)})`);
    }
  }
  return out;
}

/** dry_run: subcommands.json の dry_run の列が true の道具は boolean の dry_run を持ち、false の道具は定義に持たない(核が --dry-run を ARG_INVALID で拒否するため)。 */
export function dryRunProblems(tool: string, input: any, want: boolean): string[] {
  const schemas = propSchemas(input, "dry_run");
  if (!want) return schemas.length > 0 || argNames(input).has("dry_run") ? [`${tool}: dry_run を持たない道具(核が --dry-run を拒否する)に dry_run がある`] : [];
  if (schemas.length === 0) return [`${tool}: dry_run が無い(--dry-run を受けるサブコマンド)`];
  const out: string[] = [];
  for (const s of schemas) {
    for (const v of [true, false]) if (!accepts(s, v)) out.push(`${tool}: dry_run が ${v} を受けない`);
    for (const v of ["x", 1, {}]) if (accepts(s, v)) out.push(`${tool}: dry_run が ${JSON.stringify(v)} を受ける`);
  }
  return out;
}

/** step の値域: 署名を要する段だけを受け、署名を要さない 3 段を拒む。定義の書き方(enum / anyOf / oneOf / pattern)に依らない。 */
export function stepProblems(input: any, signedPattern: string, unsigned: string[]): string[] {
  const out: string[] = [];
  const validate = compile(input);
  const stepOk = (v: unknown) => {
    validate({ step: v });
    return !(validate.errors ?? []).some((e) => e.instancePath === "/step" || e.instancePath.startsWith("/step/"));
  };
  if (!new RegExp(signedPattern).test("V1")) out.push("見本の段 V1 が署名を要する段の pattern に合わない(試験の見本の誤り)");
  for (const s of ["R3", "R7", "V1"]) if (!stepOk(s)) out.push(`step が署名を要する段 ${s} を受けない`);
  for (const s of [...unsigned, "", "RR", "r3"]) if (stepOk(s)) out.push(`step が ${JSON.stringify(s)} を受ける`);
  return out;
}

/** secret_refs: 重複なしの文字列の配列。 */
export function secretRefsProblems(tool: string, input: any, needs: boolean): string[] {
  const schemas = propSchemas(input, "secret_refs");
  if (schemas.length === 0) return needs ? [`${tool}: secret_refs が無い(参照名を取る選択肢がある)`] : [];
  const out: string[] = [];
  for (const s of schemas) {
    if (!accepts(s, ["a", "b"])) out.push(`${tool}: secret_refs が参照名の配列を受けない`);
    for (const v of [["a", "a"], "a", [1]]) if (accepts(s, v)) out.push(`${tool}: secret_refs が ${JSON.stringify(v)} を受ける`);
  }
  return out;
}

/**
 * paths(パスを取る引数)の要素: path_ref(`pr:` と 16 進 16 桁)、人がチャットで与えた実値のパス(spec 3.7: 実値のまま道具の引数に受ける)、
 * 部分の使い方 `pr:<16 桁>/sub`(主プロセスが PATH_REF_UNKNOWN で拒む。定義の側では文字列として受ける)を受け、文字列でない要素を拒む。
 * 定義に pr: の pattern を要求しない(要求すると人が言ったパスの復元が TOOL_ARG_INVALID になる)。
 */
export function pathRefProblems(tool: string, input: any): string[] {
  const out: string[] = [];
  for (const s of propSchemas(input, "paths")) {
    for (const v of [["pr:0123456789abcdef"], ["/srv/data"], ["pr:0123456789abcdef/sub"]]) if (!accepts(s, v)) out.push(`${tool}: paths が ${JSON.stringify(v)} を受けない`);
    for (const v of [[1], [null], [{}]]) if (accepts(s, v)) out.push(`${tool}: paths が ${JSON.stringify(v)} を受ける`);
  }
  return out;
}

/** 全道具に共通: required に任意の引数(run_id・dry_run・secret_refs)を入れない。run_id は文字列。 */
export function commonProblems(tool: string, input: any): string[] {
  const out: string[] = [];
  const req: string[] = [...(input.required ?? []), ...[...(input.oneOf ?? []), ...(input.anyOf ?? [])].flatMap((b: any) => b.required ?? [])];
  for (const n of ["run_id", "dry_run", "secret_refs"]) if (req.includes(n)) out.push(`${tool}: required に任意の引数 ${n} がある`);
  for (const s of propSchemas(input, "run_id")) {
    if (!accepts(s, "r-20261018T173000Z-3f9a")) out.push(`${tool}: run_id が文字列を受けない`);
    for (const v of [1, true, {}]) if (accepts(s, v)) out.push(`${tool}: run_id が ${JSON.stringify(v)} を受ける`);
  }
  return out;
}

/** definition(構造化した定義)はオブジェクト。文字列・数・配列を拒む。 */
export function definitionProblems(tool: string, input: any): string[] {
  const out: string[] = [];
  for (const s of propSchemas(input, "definition")) {
    if (!accepts(s, { version: 1 })) out.push(`${tool}: definition がオブジェクトを受けない`);
    for (const v of ["x", 1, true, []]) if (accepts(s, v)) out.push(`${tool}: definition が ${JSON.stringify(v)} を受ける`);
  }
  return out;
}

/**
 * bootstrap(spec 3.7 の入力の表): host、port(整数)、admin_user、admin_ref、rekey(真偽値)、run_id(任意)。secret_refs は付けてよい。
 * 秘密の値を受ける引数(admin_password など)と、表に無い引数は持たない。required は host・admin_user・admin_ref の範囲(port の既定 22、rekey、run_id は任意)。
 */
export function bootstrapProblems(input: any): string[] {
  const out: string[] = [];
  const names = [...argNames(input)].filter((n) => n !== "secret_refs").sort();
  const want = ["admin_ref", "admin_user", "host", "port", "rekey", "run_id"];
  if (JSON.stringify(names) !== JSON.stringify(want)) out.push(`bootstrap の引数が表と違う: ${JSON.stringify(names)}`);
  const type = (p: string, ok: unknown[], ng: unknown[]) => {
    for (const s of propSchemas(input, p)) {
      for (const v of ok) if (!accepts(s, v)) out.push(`bootstrap.${p} が ${JSON.stringify(v)} を受けない`);
      for (const v of ng) if (accepts(s, v)) out.push(`bootstrap.${p} が ${JSON.stringify(v)} を受ける`);
    }
  };
  for (const p of ["host", "admin_user", "admin_ref"]) type(p, ["x"], [1, true, {}]);
  type("port", [22], ["22", true, 1.5, {}]);
  type("rekey", [true, false], ["x", 1, {}]);
  for (const r of input.required ?? []) if (!["host", "admin_user", "admin_ref"].includes(r)) out.push(`bootstrap の required に任意の引数 ${r} がある`);
  return out;
}

export const RESTORE_STEPS = ["place", "issue-read-key", "secrets-set", "list", "journal-verify", "restore", "revoke-read-key"];

/**
 * bootstrap-restore(spec 3.7 の入力の表): step(7 値の列挙。必須)、run_id(任意)、place の host・port(整数)・initial_user・initial_ref。
 * ほかの段の引数は 4.7 の復元の手順のサブコマンドの引数(extraAllowed: 呼び出し側が許可リストから導く)。それ以外の引数と秘密の値を受ける名前は持たない。
 */
export function bootstrapRestoreProblems(input: any, extraAllowed: string[]): string[] {
  const out: string[] = [];
  const names = [...argNames(input)];
  for (const n of ["step", "run_id", "host", "port", "initial_user", "initial_ref"]) if (!names.includes(n)) out.push(`bootstrap-restore に ${n} が無い`);
  const allowed = new Set(["step", "run_id", "secret_refs", "host", "port", "initial_user", "initial_ref", ...extraAllowed]);
  for (const n of names) if (!allowed.has(n)) out.push(`bootstrap-restore の引数 ${n} が表に無い`);
  for (const s of propSchemas(input, "step")) {
    for (const v of RESTORE_STEPS) if (!accepts(s, v)) out.push(`bootstrap-restore.step が ${v} を受けない`);
    for (const v of ["", "restore-all", "PLACE", 1]) if (accepts(s, v)) out.push(`bootstrap-restore.step が ${JSON.stringify(v)} を受ける`);
  }
  if (!(input.required ?? []).includes("step")) out.push("bootstrap-restore の required に step が無い");
  for (const r of input.required ?? []) if (["run_id", "dry_run", "secret_refs"].includes(r)) out.push(`bootstrap-restore の required に任意の引数 ${r} がある`);
  for (const s of propSchemas(input, "port")) {
    if (!accepts(s, 22)) out.push("bootstrap-restore.port が整数を受けない");
    for (const v of ["22", true]) if (accepts(s, v)) out.push(`bootstrap-restore.port が ${JSON.stringify(v)} を受ける`);
  }
  return out;
}

/**
 * propose の道具の定義の形(1 つに固定): 最上位に oneOf・anyOf・allOf を置かず、type: object、properties に kind(enum)と全種別の引数の和集合を並べ、
 * required は kind だけ、additionalProperties: false。kind ごとの引数の組み合わせは主プロセスが実行の前に検査する(定義には持たない)。
 * extraAllowed は和集合に加えてよい引数(secret_refs、dry_run)。propose は dry_run が true の列なので dry_run は必ず持つ(dryRunProblems)。
 */
export function proposeProblems(input: any, kinds: string[], unionArgs: string[], extraAllowed: string[] = ["secret_refs", "dry_run"]): string[] {
  const out: string[] = [];
  for (const k of ["oneOf", "anyOf", "allOf"]) if (k in input) out.push(`最上位に ${k} がある`);
  if (input.type !== "object") out.push("最上位の type が object でない");
  if (input.additionalProperties !== false) out.push("additionalProperties が false でない");
  if (JSON.stringify(input.required) !== JSON.stringify(["kind"])) out.push(`required が ["kind"] だけでない: ${JSON.stringify(input.required)}`);
  out.push(...dryRunProblems("sitate_propose", input, true));
  const kindEnum: string[] | undefined = input.properties?.kind?.enum;
  if (!Array.isArray(kindEnum) || JSON.stringify([...kindEnum].sort()) !== JSON.stringify([...kinds].sort())) out.push("kind の enum が許可リストの種別と一致しない");
  const have = Object.keys(input.properties ?? {}).filter((k) => !extraAllowed.includes(k)).sort();
  const want = [...new Set([...unionArgs, "kind"])].sort();
  if (JSON.stringify(have) !== JSON.stringify(want)) out.push(`properties が全種別の引数の和集合と一致しない: 差 ${JSON.stringify([...have.filter((x) => !want.includes(x)), ...want.filter((x) => !have.includes(x))])}`);
  return out;
}
