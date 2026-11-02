// 相手のホストの CPU と OS の判定(spec 3.7 の `bootstrap` の段 (2))。`uname -m` と `/etc/os-release`。
// 対応の一覧は署名済みの設定(U4 の規則ファイル)から読み替えられる形にし、既定値をここに置く。

export interface PlatformList {
  /** `/etc/os-release` の ID(と ID_LIKE)に現れてよい名前。パッケージ管理の名前の対応は apt だけ。 */
  readonly osIds: readonly string[];
  readonly arches: readonly string[];
}

export const DEFAULT_PLATFORMS: PlatformList = { osIds: ["debian", "ubuntu"], arches: ["x86_64", "arm64"] };

/** `uname -m` を配布物の `<arch>` の名前にする(aarch64 → arm64)。 */
export function archOf(unameM: string): string {
  const m = unameM.trim();
  return m === "aarch64" ? "arm64" : m;
}

export function osIdsOf(osRelease: string): string[] {
  const ids: string[] = [];
  for (const line of osRelease.split("\n")) {
    const m = /^(ID|ID_LIKE)=(.*)$/.exec(line.trim());
    if (m) ids.push(...(m[2] ?? "").replace(/^"|"$/g, "").split(/\s+/).filter((x) => x !== ""));
  }
  return ids;
}

export function supported(unameM: string, osRelease: string, list: PlatformList): { readonly ok: boolean; readonly osArch: string } {
  const arch = archOf(unameM);
  const osOk = osIdsOf(osRelease).some((id) => list.osIds.includes(id));
  return { ok: osOk && list.arches.includes(arch), osArch: `linux-${arch}` };
}
