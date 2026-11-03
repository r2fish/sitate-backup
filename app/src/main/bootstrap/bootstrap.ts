// `bootstrap`(spec 3.7)。核のサブコマンドではないので、段 (1)〜(8) と結果の封筒 `sitate.out/v1` はアプリが作る。
// 署名を検証したバイナリだけを管理アクセスで置き、`gate` を強制コマンドとして登録し、状態ディレクトリを作り、前提を整え、
// `sitate version` で確かめ、通ったときだけ管理アクセスを閉じて `attest --step admin_access_closed` を呼び、`admin_ref` を消す。
// 済んだ段は確かめて飛ばす(途中で失敗してもやり直せる)。秘密(管理アクセスの資格情報)はこのファイルの外へ出ない: 戻り値・封筒・例外に載せない。
import { generateReadableKeyPair } from "../ssh/keygen.ts";
import { createHash } from "node:crypto";
import ssh2 from "ssh2";
import { DEFAULT_SSH_PORT } from "../../constants.ts";
import { appErrorEnvelope } from "../app-envelope.ts";
import type { AppEnvelope, AppErrorCode } from "../app-envelope.ts";
import { declaredExes } from "../bundle-verify.ts";
import { verifyMinisign } from "../minisign.ts";
import type { TrustedKey } from "../minisign.ts";
import { parseStdoutRecords } from "../stdout-records.ts";
import { SshConnection, SshError } from "../ssh/connection.ts";
import type { Connected } from "../ssh/connection.ts";
import { newRunId } from "../ssh/core-runner.ts";
import type { TargetStore } from "../ssh/target-store.ts";
import { DEFAULT_PLATFORMS, supported } from "./platform.ts";
import type { PlatformList } from "./platform.ts";
import { LIBEXEC, LIMITS_DROPIN, LIMITS_DROPIN_BODY, Remote, RemoteDisconnected, SITATE_BIN, SITATE_USER, SSHD_DROPIN, SSHD_DROPIN_BODY, STATE_DIR, shq } from "./remote.ts";

const { utils } = ssh2;

export interface BundleReader {
  readonly version: string;
  /** `bundle/<version>/` の中の接頭辞に一致する相対パス(署名ファイルを除く)。 */
  list(prefix: string): string[];
  read(rel: string): { readonly bytes: Buffer; readonly sig: string } | null;
}

export interface BootstrapInput {
  readonly host: string;
  /** 省略時は DEFAULT_SSH_PORT。 */
  readonly port?: number | undefined;
  readonly admin_user: string;
  readonly admin_ref: string;
  readonly rekey: boolean;
  readonly run_id?: string | undefined;
}

export interface BootstrapDeps {
  readonly targets: TargetStore;
  readonly bundle: BundleReader;
  readonly keys: readonly TrustedKey[];
  /** 管理アクセスの資格情報(OpenSSH 形式の秘密鍵かパスワード)。無ければ null。 */
  readonly adminCredential: () => Promise<string | null>;
  readonly clearAdminCredential: () => Promise<void>;
  /** 管理用 SSH 鍵(キーチェーンの 1 項目)。 */
  readonly loadAdminKey: () => Promise<string | null>;
  readonly saveAdminKey: (openSshPrivateKey: string) => Promise<void>;
  /** 人が「アプリの情報」の区画で入れた復旧キットの指紋。無ければ null。 */
  readonly typedFingerprint: () => string | null;
  readonly platforms?: PlatformList;
  readonly connectTimeoutMs: number;
  readonly now?: () => Date;
}

type ResolvedInput = BootstrapInput & { readonly port: number };

class Fail extends Error {
  readonly code: AppErrorCode;
  readonly detail: Readonly<Record<string, unknown>>;
  constructor(code: AppErrorCode, detail: Readonly<Record<string, unknown>> = {}) {
    super(code);
    this.code = code;
    this.detail = detail;
  }
}

const sha256 = (b: Buffer): string => createHash("sha256").update(b).digest("hex");

export async function runBootstrap(raw: BootstrapInput, deps: BootstrapDeps): Promise<Record<string, unknown>> {
  const input: ResolvedInput = { ...raw, port: raw.port ?? DEFAULT_SSH_PORT };
  const runId = input.run_id ?? newRunId(deps.now?.());
  const fail = (code: AppErrorCode, detail: Readonly<Record<string, unknown>> = {}): AppEnvelope => appErrorEnvelope(code, ["bootstrap"], detail, runId, { firstBootstrap: !input.rekey });
  const conn = new SshConnection();
  try {
    return await steps(input, deps, conn, runId);
  } catch (e) {
    if (e instanceof Fail) return fail(e.code, e.detail) as unknown as Record<string, unknown>;
    if (e instanceof SshError) {
      const f = e.failure;
      return fail(f.code, f.code === "SSH_UNREACHABLE" ? { reason: f.reason } : {}) as unknown as Record<string, unknown>;
    }
    if (e instanceof RemoteDisconnected) return fail("SSH_DISCONNECTED") as unknown as Record<string, unknown>;
    throw e;
  } finally {
    conn.close();
  }
}

async function steps(input: ResolvedInput, deps: BootstrapDeps, conn: SshConnection, runId: string): Promise<Record<string, unknown>> {
  const c = await connectAdmin(input, deps, conn);
  const r = new Remote(conn);

  // (2) CPU と OS、(3) root の権限。
  const [um, osr] = [await r.run("uname -m"), await r.run("cat /etc/os-release")];
  const plat = supported(um.out, osr.out, deps.platforms ?? DEFAULT_PLATFORMS);
  if (um.code !== 0 || osr.code !== 0 || !plat.ok) throw new Fail("BOOTSTRAP_PLATFORM_UNSUPPORTED");
  if ((await r.run("id -u")).out.trim() !== "0") {
    r.sudo = "sudo -n ";
    if ((await r.root("true")).code !== 0) throw new Fail("BOOTSTRAP_PRIVILEGE_REQUIRED");
  }

  // 済みの確認。--rekey は sitate ユーザーと gate が前提。
  const hasUser = (await r.run(`id ${SITATE_USER}`)).code === 0;
  const hasBin = (await r.run(`test -x ${shq(SITATE_BIN)}`)).code === 0;
  if (input.rekey && !(hasUser && hasBin)) throw new Fail("BOOTSTRAP_NOT_DONE");

  // (4) 署名を検証した核とアダプタと価格表を置く。--rekey は鍵の差し替えだけ。
  if (!input.rekey) await place(r, deps, plat.osArch);
  // (5) 前提。
  if (!input.rekey) await prerequisites(r);

  // (6) 管理用 SSH 鍵を作り、強制コマンド付きの行を登録する。
  const stored = await deps.loadAdminKey();
  const keyPair = !input.rekey && stored !== null ? stored : null;
  const generated = keyPair === null ? generateReadableKeyPair() : null;
  const priv = keyPair ?? generated!.private;
  const parsed = utils.parseKey(priv);
  if (parsed instanceof Error) throw new Fail("BOOTSTRAP_NOT_DONE");
  const pubLine = `ssh-ed25519 ${parsed.getPublicSSH().toString("base64")}`;
  const replaced = await registerKey(r, input.rekey, pubLine, async () => { if (generated !== null) await deps.saveAdminKey(priv); });
  deps.targets.write({ ...(deps.targets.read() ?? { host: input.host, port: input.port, user: SITATE_USER, hostKeyFp: c.hostKeyFp }), adminPublicKey: pubLine });

  // (7) 管理用 SSH で `sitate version`。
  const ver = await verifyNewKey(input, deps, priv, c.hostKeyFp);

  // (8) (7) が通ったときだけ管理アクセスを閉じる。
  const closed = await closeAccess(r, (await deps.adminCredential()) ?? "", input.admin_user);
  if (closed) await deps.clearAdminCredential();
  return {
    schema: "sitate.out/v1", command: ["bootstrap"], ok: true, dry_run: false, run_id: runId,
    result: { host_key_fp: c.hostKeyFp, host_key_pinned: c.pinnedNow, core_version: ver.version, signing_key_fp: ver.signingKeyFp, admin_key_type: "software", rekeyed: input.rekey, replaced_key_lines: replaced, admin_access_closed: closed },
    warnings: [], error: null, next: [],
  };
}

/** 段 (1): 管理アクセスで接続し、ホスト鍵を固定または照合する。 */
async function connectAdmin(input: ResolvedInput, deps: BootstrapDeps, conn: SshConnection): Promise<Connected> {
  const cred = await deps.adminCredential();
  if (cred === null) throw new Fail("SSH_AUTH_DENIED");
  const existing = deps.targets.read();
  // 人が登録した接続先とモデルの host・port が違えば、指紋の有無によらず止める(管理パスワードを別のホストへ送らない)。
  if (existing !== null && (existing.host !== input.host || existing.port !== input.port)) throw new Fail("SSH_HOSTKEY_MISMATCH");
  let c: Connected;
  try {
    c = await conn.connect({
      host: input.host, port: input.port, username: input.admin_user,
      auth: cred.includes("PRIVATE KEY") ? { privateKey: cred } : { password: cred },
      pinnedFp: existing?.hostKeyFp ?? null, typedFp: deps.typedFingerprint(),
      // 新しい端末からの --rekey は人が入れた指紋と一致したときだけ固定する。最初の bootstrap は初回接続で固定する。
      allowFirstUse: !input.rekey, connectTimeoutMs: deps.connectTimeoutMs,
    });
  } catch (e) {
    // 認証が通らなかったときは、失敗した参照名の値をキーチェーンから消す(次の呼び出しで人が入れ直す。spec 3.7)。
    if (e instanceof SshError && e.failure.code === "SSH_AUTH_DENIED" && !input.rekey) await deps.clearAdminCredential();
    throw e;
  }
  if (c.pinnedNow) deps.targets.write({ host: input.host, port: input.port, user: SITATE_USER, hostKeyFp: c.hostKeyFp, adminPublicKey: existing?.adminPublicKey ?? null });
  return c;
}

/** 段 (4): 署名を検証して置き、置いた後の SHA-256 を照らす。 */
async function place(r: Remote, deps: BootstrapDeps, osArch: string): Promise<void> {
  const { bundle, keys } = deps;
  const archOf = (rel: string): string => /^(linux-[a-z0-9_]+)\//.exec(rel)?.[1] ?? "any";
  const check = (rel: string, arch: string): Buffer => {
    const f = bundle.read(rel);
    if (f === null) throw new Fail("BOOTSTRAP_SIGNATURE_INVALID");
    const v = verifyMinisign(f.bytes, f.sig, keys, { version: bundle.version, arch, path: rel });
    if (!v.ok) throw new Fail("BOOTSTRAP_SIGNATURE_INVALID");
    return f.bytes;
  };
  const putChecked = async (dest: string, bytes: Buffer, mode: string): Promise<void> => {
    if (!(await r.put(dest, bytes, mode)) || (await r.sha256(dest)) !== sha256(bytes)) throw new Fail("BOOTSTRAP_SIGNATURE_INVALID");
  };
  const putSig = (dest: string, rel: string): Promise<void> => putChecked(`${dest}.sig`, Buffer.from(bundle.read(rel)!.sig, "utf8"), "0644");
  // 配布物の全体(両方の arch、ソースのアーカイブ、SBOM、価格表)を版ディレクトリへ置く(spec 3.7 の bootstrap、4.2。phrase verify が保管先の bin/ へ写す元)。
  // 署名があるものは署名を検証して .sig ごと置く。アダプタの実行ファイルは署名せず、署名付きの adapter.json の sha256 で照らす(spec 3.2.1)。
  // 配布物 bundle/<version>/ の中身だけ(spec 4.2): <os>-<arch>/、prices/、版ディレクトリ直下のソースのアーカイブと SBOM。アプリの同梱物(手順書・道具の定義など)は含まない。
  const rels = bundle.list("").filter((rel) => /^linux-[a-z0-9_]+\//.test(rel) || rel.startsWith("prices/") || /^[^/]+-src\.tar\.gz$/.test(rel) || /^[^/]+\.spdx\.json$/.test(rel));
  const files = new Map<string, Buffer>();
  const exes = declaredExes(rels.flatMap((rel) => (rel.endsWith("/adapter.json") ? [{ path: rel, bytes: check(rel, archOf(rel)), sig: "" }] : [])));
  for (const rel of rels) {
    if (exes.has(rel)) {
      const f = bundle.read(rel);
      if (f === null || sha256(f.bytes) !== exes.get(rel)) throw new Fail("BOOTSTRAP_SIGNATURE_INVALID");
      files.set(rel, f.bytes);
    } else files.set(rel, check(rel, archOf(rel)));
  }
  const vdir = `${LIBEXEC}/bundle/${bundle.version}`;
  const modeOf = (rel: string): string => (exes.has(rel) || rel.endsWith("/sitate") ? "0755" : "0644");
  const core = files.get(`${osArch}/sitate`);
  if (core === undefined) throw new Fail("BOOTSTRAP_SIGNATURE_INVALID");
  await putChecked(SITATE_BIN, core, "0755");
  for (const [rel, bytes] of files) {
    await putChecked(`${vdir}/${rel}`, bytes, modeOf(rel));
    if (!exes.has(rel)) await putSig(`${vdir}/${rel}`, rel);
  }
  // 動いているホストの <os>-<arch> のアダプタと価格表は、核が使う置き場へも置く(署名を付け直さない)。
  for (const rel of rels.filter((x) => x.startsWith(`${osArch}/adapters/`))) {
    const dest = `${LIBEXEC}/${rel.slice(osArch.length + 1)}`;
    await putChecked(dest, files.get(rel)!, modeOf(rel));
    if (!exes.has(rel)) await putSig(dest, rel);
  }
  for (const rel of rels.filter((x) => x.startsWith("prices/"))) {
    await putChecked(`${LIBEXEC}/${rel}`, files.get(rel)!, "0644");
    await putSig(`${LIBEXEC}/${rel}`, rel);
  }
}

/** 段 (5): 専用ユーザー、rootless の podman、タイマーの常駐、状態ディレクトリとロックファイル。済んだものは飛ばす。 */
async function prerequisites(r: Remote): Promise<void> {
  const stepsList: [string, string][] = [
    ["packages", "command -v podman >/dev/null 2>&1 || (apt-get update -y && DEBIAN_FRONTEND=noninteractive apt-get install -y podman uidmap slirp4netns)"],
    ["user", `id ${SITATE_USER} >/dev/null 2>&1 || useradd --system --create-home --home-dir ${STATE_DIR} --shell /bin/bash ${SITATE_USER}`],
    ["subids", `grep -q '^${SITATE_USER}:' /etc/subuid || usermod --add-subuids 200000-265535 --add-subgids 200000-265535 ${SITATE_USER}`],
    ["state_dir", `install -d -m 0700 -o ${SITATE_USER} -g ${SITATE_USER} ${STATE_DIR} ${STATE_DIR}/run && for f in run.lock state.lock; do [ -e ${STATE_DIR}/$f ] || install -m 0600 -o ${SITATE_USER} -g ${SITATE_USER} /dev/null ${STATE_DIR}/$f; done`],
    // 定義ファイルの置き場(seal と def-change の apply が sitate として書く。D-0164)。install -d は繰り返しても同じ形になる。
    ["etc_dir", `install -d -m 0750 -o ${SITATE_USER} -g ${SITATE_USER} /etc/sitate`],
    ["linger", `loginctl enable-linger ${SITATE_USER}`],
  ];
  for (const [step, cmd] of stepsList) if ((await r.root(cmd)).code !== 0) throw new Fail("BOOTSTRAP_PACKAGE_FAILED", { step });
  // memlock の上限(同じ内容で置き直すので繰り返しても変わらない)。効くかどうかは sshd の PAM 次第で、ここでは置くだけ。
  const lim = await r.root(`cat > ${shq(LIMITS_DROPIN)} && chmod 0644 ${shq(LIMITS_DROPIN)}`, LIMITS_DROPIN_BODY);
  if (lim.code !== 0) throw new Fail("BOOTSTRAP_PACKAGE_FAILED", { step: "memlock" });
}

/** 段 (6): authorized_keys の管理用の行を登録する。置き換えた行の数を返す。別の鍵の行が既にあれば(rekey でなければ)BOOTSTRAP_ALREADY_DONE。 */
async function registerKey(r: Remote, rekey: boolean, pubLine: string, persist: () => Promise<void>): Promise<number> {
  const ak = `${STATE_DIR}/.ssh/authorized_keys`;
  const cur = await r.root(`cat ${shq(ak)} 2>/dev/null || true`);
  const lines = cur.out.split("\n").filter((l) => l.trim() !== "");
  const admin = lines.filter((l) => l.includes(`${SITATE_BIN} gate`));
  const ours = admin.some((l) => l.includes(pubLine));
  if (!rekey && admin.length > 0 && !ours) throw new Fail("BOOTSTRAP_ALREADY_DONE");
  if (!rekey && ours) return 0;
  await persist(); // 登録の前にキーチェーンへ(登録だけ済んで秘密鍵を失うのを避ける)。
  const keep = lines.filter((l) => !admin.includes(l));
  const body = [...keep, `command="${SITATE_BIN} gate",restrict ${pubLine} sitate-admin`].join("\n") + "\n";
  const ok = await r.root(`install -d -m 0700 -o ${SITATE_USER} -g ${SITATE_USER} ${shq(`${STATE_DIR}/.ssh`)} && cat > ${shq(ak)} && chmod 0600 ${shq(ak)} && chown ${SITATE_USER}:${SITATE_USER} ${shq(ak)}`, body);
  if (ok.code !== 0) throw new Fail("BOOTSTRAP_PACKAGE_FAILED", { step: "authorized_keys" });
  return admin.length;
}

/** 段 (7): 新しい鍵で管理用 SSH を張り、強制コマンド経由で `version` を呼ぶ。固定値と照らす。 */
async function verifyNewKey(input: ResolvedInput, deps: BootstrapDeps, priv: string, hostKeyFp: string): Promise<{ version: string; signingKeyFp: string }> {
  const c2 = new SshConnection();
  try {
    await c2.connect({ host: input.host, port: input.port, username: SITATE_USER, auth: { privateKey: priv }, pinnedFp: hostKeyFp, typedFp: null, allowFirstUse: false, connectTimeoutMs: deps.connectTimeoutMs });
    const out = await c2.exec("version");
    const rec = parseStdoutRecords(out.stdout.toString("utf8"), out.disconnected ? "ssh_disconnected" : "complete");
    const res = rec.ok && rec.envelope.ok === true ? (rec.envelope.result as { version?: unknown; signing_key_fp?: unknown } | null) : null;
    if (res === null || typeof res.version !== "string") throw new Fail("SSH_AUTH_DENIED");
    return { version: res.version, signingKeyFp: typeof res.signing_key_fp === "string" ? res.signing_key_fp : "" };
  } finally {
    c2.close();
  }
}

/** 段 (8): パスワードログインを無効にし、管理用 OS アカウントの鍵の行を外し、記録を残す。閉じられなければ false(記録しない)。 */
async function closeAccess(r: Remote, cred: string, adminUser: string): Promise<boolean> {
  const w = await r.root(`cat > ${shq(SSHD_DROPIN)} && chmod 0644 ${shq(SSHD_DROPIN)} && (systemctl reload ssh 2>/dev/null || systemctl reload sshd)`, SSHD_DROPIN_BODY);
  if (w.code !== 0) return false;
  if (cred.includes("PRIVATE KEY")) {
    const k = utils.parseKey(cred);
    if (k instanceof Error) return false;
    const b64 = k.getPublicSSH().toString("base64");
    const rm = await r.root(`h=$(getent passwd ${shq(adminUser)} | cut -d: -f6); f="$h/.ssh/authorized_keys"; if [ -f "$f" ]; then grep -vF ${shq(b64)} "$f" > "$f.new"; cat "$f.new" > "$f"; rm -f "$f.new"; fi; ! grep -qF ${shq(b64)} "$f" 2>/dev/null`);
    if (rm.code !== 0) return false;
  }
  // 記録は核が残す(アプリは状態ファイルを直接書かない)。失敗しても閉じたことは変わらない(doctor が不合格になり、人がコンソールから同じコマンドを呼ぶ)。
  await r.asSitate("attest --step admin_access_closed");
  return true;
}
