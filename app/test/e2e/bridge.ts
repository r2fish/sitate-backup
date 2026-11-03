// 作業端末(macOS)から受入試験の環境のコンテナ(SSH 22 番。ホストのポートは公開しない)へ届く口を作る。
// 作業端末の 127.0.0.1:<空きポート> を、`docker exec -i <コンテナ> bash -c '… /dev/tcp/127.0.0.1/22 …'` へつなぐ。試験用のアプリは SITATE_TEST_HOST_MAP で接続先だけを読み替える。
import { spawn } from "node:child_process";
import { createServer } from "node:net";
import type { Server } from "node:net";
import type { AddressInfo } from "node:net";

const RELAY = 'exec 3<>/dev/tcp/127.0.0.1/22; cat <&3 & cat >&3; sleep 0.3; kill $! 2>/dev/null; exit 0';

export interface Bridge { readonly hostMap: string; close(): void }

export async function startBridge(containers: Readonly<Record<string, string>>): Promise<Bridge> {
  const servers: Server[] = [];
  const parts: string[] = [];
  for (const [role, container] of Object.entries(containers)) {
    const server = createServer((sock) => {
      const child = spawn("docker", ["exec", "-i", container, "bash", "-c", RELAY], { stdio: ["pipe", "pipe", "ignore"] });
      sock.pipe(child.stdin);
      child.stdout.pipe(sock);
      const end = (): void => { sock.destroy(); child.kill(); };
      sock.on("close", end);
      sock.on("error", end);
      child.stdin.on("error", () => undefined);
      child.on("exit", () => sock.end());
    });
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
    servers.push(server);
    parts.push(`${role}=127.0.0.1:${(server.address() as AddressInfo).port}`);
  }
  return { hostMap: parts.join(","), close: () => { for (const s of servers) s.close(); } };
}
