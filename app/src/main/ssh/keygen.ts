import ssh2 from "ssh2";

const { utils } = ssh2;

// ssh2 が作る Ed25519 の鍵は、まれに ssh2 自身が読めない(実測で 2000 個に 7 個)。読める鍵が出るまで作り直す。
const KEYGEN_ATTEMPTS = 8;

type KeyPair = { readonly private: string; readonly public: string };

export function generateReadableKeyPair(gen: () => KeyPair = () => utils.generateKeyPairSync("ed25519")): KeyPair {
  for (let i = 0; i < KEYGEN_ATTEMPTS; i++) {
    const k = gen();
    if (!(utils.parseKey(k.private) instanceof Error)) return k;
  }
  throw new Error("KEYGEN_FAILED");
}
