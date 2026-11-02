// 接続先ファイル(spec 3.7「アプリのデータの置き場」)。接続先(ホスト、ポート、ユーザー)と固定したホスト鍵の指紋、管理用公開鍵の写し。秘密ではない。
// 固定値は人が接続先を削除したときだけ捨てる。固定済みの値を上書きする経路を持たない(違う指紋は SSH_HOSTKEY_MISMATCH で止まる)。
import { mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";

export interface Target {
  readonly host: string;
  readonly port: number;
  readonly user: string;
  readonly hostKeyFp: string | null;
  readonly adminPublicKey: string | null;
}

export class TargetStore {
  readonly #path: string;
  constructor(path: string) {
    this.#path = path;
  }

  read(): Target | null {
    try {
      const j = JSON.parse(readFileSync(this.#path, "utf8")) as Partial<Target>;
      if (typeof j.host !== "string" || typeof j.port !== "number" || typeof j.user !== "string") return null;
      return { host: j.host, port: j.port, user: j.user, hostKeyFp: typeof j.hostKeyFp === "string" ? j.hostKeyFp : null, adminPublicKey: typeof j.adminPublicKey === "string" ? j.adminPublicKey : null };
    } catch {
      return null;
    }
  }

  write(t: Target): void {
    mkdirSync(dirname(this.#path), { recursive: true });
    const tmp = this.#path + ".tmp";
    writeFileSync(tmp, JSON.stringify(t, null, 2), { mode: 0o600 });
    renameSync(tmp, this.#path);
  }

  /** 固定する。すでに別の値で固定済みなら何もしない(false)。 */
  pin(fp: string): boolean {
    const t = this.read();
    if (t === null || (t.hostKeyFp !== null && t.hostKeyFp !== fp)) return false;
    this.write({ ...t, hostKeyFp: fp });
    return true;
  }

  /** 人の操作(接続先の削除)だけが呼ぶ。 */
  remove(): void {
    rmSync(this.#path, { force: true });
  }
}
