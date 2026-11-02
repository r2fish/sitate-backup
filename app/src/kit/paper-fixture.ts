// 紙の試験の入力(試験専用)。`fields` は internal/keys/kitfields.go が出す形(手順は 1・2・3・4・5・6 だけ、(3) と (5) の argv に "$RUN")に合わせる。
import { join } from "node:path";
import { readJson, repoRoot } from "../../test/contract/helpers.ts";
import type { KitFields, PhraseWords } from "./types.ts";

export const PHRASE = ["alpha1", "bravo2", "carol3", "delta4", "echo05", "foxtr6", "golf07", "hotel8", "india9", "julia10", "kilo11", "lima12", "mike13", "nov14", "oscar15", "papa16", "quebec17", "romeo18", "sierra19", "tango20", "unif21", "victor22", "whisk23", "xray24"] as unknown as PhraseWords;

export function vectorFields(): KitFields {
  const s = readJson(join(repoRoot, "internal/contract/testdata/cli/results-samples.json"));
  return s.results.kit.fields as KitFields;
}

const hex = (c: string) => c.repeat(64);

/** 核の出力の形の fields。値は札ごとに別にして、入れ替えを見分けられるようにする。 */
export function realFields(over: Partial<KitFields["storage"]> = {}): KitFields {
  const f = vectorFields();
  const st = { ...f.storage, provider: "b2", country: null, endpoint: "https://s3.us-west-004.example.invalid", bucket: "bkt-main", prefix: "pfx/office/", repo_id: "repo-77", keyring_id: "kr-5", key_ref: "storage-ref", ...over };
  const run = ["sudo", "-u", "sitate", "sitate"];
  const conn = ["--endpoint", st.endpoint, "--bucket", st.bucket, "--prefix", st.prefix];
  const ref = ["--key-ref", st.key_ref, "--repo-id", st.repo_id];
  const kr = ["--keyring", st.keyring_id];
  return {
    ...f,
    storage: st,
    core_sha256: { linux_x86_64: hex("a"), linux_arm64: hex("b") },
    sbom_sha256: hex("c"),
    source_sha256: hex("d"),
    signing_keys: [{ public_key: "RWQkeyone" + "A".repeat(40), fp: "SHA256:fpone" }, { public_key: "RWQkeytwo" + "B".repeat(40), fp: "SHA256:fptwo" }],
    steps: [
      { id: "1", argv: [] },
      { id: "2", argv: [] },
      { id: "3", argv: [...run, "storage", "issue-read-key", ...conn, "--adapter", st.provider, "--admin-ref", "admin", ...ref, "--run-id", "$RUN", "--secrets-stdin"] },
      { id: "4", argv: [...run, "list", ...conn, ...kr, ...ref, "--phrase-stdin"] },
      { id: "5", argv: [...run, "restore", ...conn, ...kr, ...ref, "--phrase-stdin", "--backup", "latest", "--to", "/var/lib/sitate/restore/$RUN/to"] },
      { id: "6", argv: [...run, "storage", "revoke-read-key", ...conn, "--adapter", st.provider, "--admin-ref", "admin", "--key-ref", st.key_ref, "--secrets-stdin"] },
    ],
  };
}
