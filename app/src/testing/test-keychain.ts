// 試験用のアプリだけ(`make build-app-test` が束ねる。製品のビルドは束ねない)。環境変数 SITATE_TEST_KEYCHAIN が使い捨ての keychain のパスを指すとき、
// `security` の全命令の末尾にそのパスを足して、利用者の既定の keychain に触れない(起動の煙試験が承認鍵などを書くため)。
import { spawnSecurity } from "../main/keychain.ts";
import type { SecurityExec } from "../main/keychain.ts";

export function securityForTest(): SecurityExec | undefined {
  const kc = process.env.SITATE_TEST_KEYCHAIN;
  if (kc === undefined || kc === "" || /["\n\r\\]/.test(kc)) return undefined;
  return (args, stdin) => (args[0] === "-i"
    ? spawnSecurity(args, stdin.replace(/\n$/, "") + ` "${kc}"\n`)
    : spawnSecurity([...args, kc], stdin));
}
