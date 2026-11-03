// 受入試験の環境のコンテナ(役割名 = ホスト名。port 22)は作業端末から直接届かない。ドライバが作業端末の口(127.0.0.1:<port>)へ橋渡しし、
// 試験用のアプリはその写像(`名前=ホスト:ポート,…`)で管理用 SSH の接続先だけを読み替える。核が使う名前(定義の source.host など)は変えない。

export function parseHostMap(spec: string | undefined): (host: string, port: number) => { host: string; port: number } {
  const table = new Map<string, { host: string; port: number }>();
  for (const part of (spec ?? "").split(",")) {
    const m = /^([^=]+)=([^:]+):(\d+)$/.exec(part.trim());
    if (m !== null) table.set(m[1]!, { host: m[2]!, port: Number(m[3]) });
  }
  return (host, port) => table.get(host) ?? { host, port };
}
