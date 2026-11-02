// 接続先の登録・削除と復旧キットの指紋の入力(状態と報告の表示の「アプリの情報」の区画。spec 3.7 の管理用 SSH の方式)。
// 登録する値はホストとポートと、人が復旧キットから写した指紋。指紋は形を確かめて固定し、固定済みの値は上書きしない(違えば接続が SSH_HOSTKEY_MISMATCH で止まる)。
// 変えたいときは人が接続先を削除してから登録し直す。管理用 SSH 鍵と資格情報はここに触れない。
import { normalizeFingerprint } from "./ssh/fingerprint.ts";
import type { Target, TargetStore } from "./ssh/target-store.ts";

/** バックアップサーバー側の核を実行する利用者(bootstrap が作る)。 */
export const TARGET_USER = "sitate";

export type TargetResult = { readonly ok: true } | { readonly ok: false; readonly reason: "host" | "port" | "fingerprint" | "exists" | "none" };

export interface TargetView {
  readonly registered: boolean;
  readonly host: string | null;
  readonly port: number | null;
  readonly fingerprintPinned: boolean;
  readonly notice: string | null;
  readonly labels: Readonly<Record<"heading" | "host" | "port" | "fingerprint" | "register" | "remove" | "pinned", string>>;
}

const HOST_RE = /^[A-Za-z0-9._:][A-Za-z0-9._:-]{0,252}$/;

export class TargetActions {
  readonly #targets: TargetStore;
  readonly #typedFp: { current: string | null };
  readonly #label: (key: string) => string;
  readonly #changed: () => void;
  #notice: string | null = null;

  constructor(deps: { targets: TargetStore; typedFp: { current: string | null }; label: (key: string) => string; onChange: () => void }) {
    this.#targets = deps.targets;
    this.#typedFp = deps.typedFp;
    this.#label = deps.label;
    this.#changed = deps.onChange;
  }

  /** 登録。すでにあれば断る(上書きしない)。指紋は省略でき、後から入れて固定できる。 */
  register(input: { host: unknown; port: unknown; fingerprint?: unknown }): TargetResult {
    const host = typeof input.host === "string" ? input.host.trim() : "";
    if (!HOST_RE.test(host)) return this.#done({ ok: false, reason: "host" });
    const port = typeof input.port === "number" ? input.port : typeof input.port === "string" && /^[0-9]{1,5}$/.test(input.port.trim()) ? Number(input.port.trim()) : NaN;
    if (!Number.isInteger(port) || port < 1 || port > 65535) return this.#done({ ok: false, reason: "port" });
    const fpText = typeof input.fingerprint === "string" ? input.fingerprint.trim() : "";
    const fp = fpText === "" ? null : normalizeFingerprint(fpText);
    if (fpText !== "" && fp === null) return this.#done({ ok: false, reason: "fingerprint" });
    if (this.#targets.read() !== null) return this.#done({ ok: false, reason: "exists" });
    const t: Target = { host, port, user: TARGET_USER, hostKeyFp: fp, adminPublicKey: null };
    this.#targets.write(t);
    if (fp !== null) this.#typedFp.current = fp;
    return this.#done({ ok: true });
  }

  /** 指紋の入力。登録済みで未固定ならその場で固定し、固定済みなら変えない(`exists`)。登録が無ければ bootstrap の初回の照合に使うため保持する。 */
  setFingerprint(input: unknown): TargetResult {
    const fp = typeof input === "string" ? normalizeFingerprint(input) : null;
    if (fp === null) return this.#done({ ok: false, reason: "fingerprint" });
    const cur = this.#targets.read();
    if (cur === null) { this.#typedFp.current = fp; return this.#done({ ok: true }); }
    if (cur.hostKeyFp !== null) return this.#done({ ok: false, reason: "exists" });
    this.#targets.pin(fp);
    this.#typedFp.current = fp;
    return this.#done({ ok: true });
  }

  /** 削除。固定した指紋も捨てる(人の操作だけが呼ぶ)。 */
  remove(): TargetResult {
    if (this.#targets.read() === null) return this.#done({ ok: false, reason: "none" });
    this.#targets.remove();
    this.#typedFp.current = null;
    return this.#done({ ok: true });
  }

  view(): TargetView {
    const t = this.#targets.read();
    const l = this.#label;
    return {
      registered: t !== null, host: t?.host ?? null, port: t?.port ?? null, fingerprintPinned: t?.hostKeyFp != null, notice: this.#notice,
      labels: { heading: l("status.target.heading"), host: l("status.target.host"), port: l("status.target.port"), fingerprint: l("status.target.fingerprint"), register: l("status.target.register"), remove: l("status.target.remove"), pinned: l("status.target.pinned") },
    };
  }

  #done(r: TargetResult): TargetResult {
    this.#notice = r.ok ? null : this.#label(`status.target.invalid.${r.reason}`);
    this.#changed();
    return r;
  }
}
