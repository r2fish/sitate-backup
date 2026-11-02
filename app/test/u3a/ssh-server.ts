// 試験用の SSH サーバー(ssh2 の Server。ローカルの空きポートで待つ)。実物の sshd の代わりに、ホスト鍵の照合・認証・exec の入出力・切断を確かめる。
import { generateReadableKeyPair } from "../../src/main/ssh/keygen.ts";
import ssh2 from "ssh2";
import type { AddressInfo } from "node:net";

const { Server, utils } = ssh2;

export interface TestServer {
  readonly port: number;
  readonly hostKeyBlob: Buffer;
  readonly clientPrivate: string;
  readonly requests: string[];
  close(): Promise<void>;
}

export interface ServerOptions {
  readonly keyType?: "ed25519" | "rsa";
  /** true なら Ed25519 の公開鍵ならどれでも受ける(bootstrap が作る新しい鍵の試験用)。 */
  readonly acceptAnyKey?: boolean;
  /** exec を受けたときの動作。stdin は端が閉じるまで集めて渡す。 */
  readonly onExec?: (command: string, stdin: Buffer, stream: ssh2.ServerChannel) => void;
}

export async function startServer(opts: ServerOptions = {}): Promise<TestServer> {
  const host = (opts.keyType === "rsa" ? utils.generateKeyPairSync("rsa", { bits: 2048 }) : generateReadableKeyPair());
  const client = generateReadableKeyPair();
  const clientPub = utils.parseKey(client.public);
  if (clientPub instanceof Error) throw clientPub;
  const requests: string[] = [];
  const server = new Server({ hostKeys: [host.private] }, (c) => {
    c.on("error", () => undefined);
    c.on("authentication", (ctx) => {
      if (ctx.method === "publickey" && opts.acceptAnyKey === true && ctx.key.algo === "ssh-ed25519") return ctx.accept();
      if (ctx.method === "publickey" && ctx.key.algo === clientPub.type && ctx.key.data.equals(clientPub.getPublicSSH())) return ctx.accept();
      if (ctx.method === "none") return ctx.reject(["publickey"]);
      return ctx.reject(["publickey"]);
    });
    c.on("ready", () => {
      c.on("session", (accept) => {
        const s = accept();
        for (const ev of ["pty", "shell", "subsystem", "auth-agent", "x11", "env"] as const) s.on(ev, (acc: unknown, rej: unknown) => { requests.push(ev); (rej as (() => void) | undefined)?.(); void acc; });
        s.on("exec", (acc, _rej, info) => {
          requests.push("exec");
          const stream = acc();
          const chunks: Buffer[] = [];
          stream.on("data", (d: Buffer) => chunks.push(d));
          stream.on("end", () => (opts.onExec ?? ((cmd, _in, st) => { st.write(cmd); st.exit(0); st.end(); }))(info.command, Buffer.concat(chunks), stream));
        });
      });
      c.on("request", (_acc: unknown, rej: () => void, name: string) => { requests.push(name); rej(); });
    });
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
  const port = (server.address() as AddressInfo).port;
  const hostPub = utils.parseKey(host.public);
  if (hostPub instanceof Error) throw hostPub;
  return {
    port, hostKeyBlob: hostPub.getPublicSSH(), clientPrivate: client.private, requests,
    close: () => new Promise<void>((r) => server.close(() => r())),
  };
}
