// 試験用のアプリへのドライバの取り付け(`make build-app-test` だけが束ねる)。SITATE_TEST_DRIVER_SOCKET が無ければ何もしない。
import { startDriverServer } from "./driver-server.ts";

export function attachDriver(evalInPage: (script: string) => Promise<unknown>): void {
  const sock = process.env.SITATE_TEST_DRIVER_SOCKET;
  if (sock !== undefined) startDriverServer(sock, { eval: evalInPage }, () => undefined);
}
