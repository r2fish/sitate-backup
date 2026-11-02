package keys

// kit の fields(schema/data/kit-fields.json)の組み立て。入力が欠けているときは、欠けた値を空や 0 で埋めず INTERNAL で止める
// (紙に刷る値を推測で埋めない)。

import (
	"context"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// binPrefix は保管先の bin/ の置き場(`<p>bin/`)。prefix は `<p>`(末尾の / を含む形)。
func binPrefix(prefix string) string { return prefix + "bin/" }

// keyNameBase は製品の印の名前の `<p>` 部(prefix の / を - に替えて末尾の - を落としたもの。spec 3.2.6)。
func keyNameBase(prefix string) string {
	return strings.TrimRight(strings.ReplaceAll(prefix, "/", "-"), "-")
}

func (d kitData) fields(clock contract.Clock, ctx context.Context) (map[string]any, error) {
	t := d.Target
	if !d.HaveBundle || d.CoreSHA["linux-x86_64"] == "" || d.CoreSHA["linux-arm64"] == "" || d.SBOM == "" || d.Src == "" || d.HostFP == "" ||
		len(d.SignKeys) == 0 || t.Endpoint == "" || t.Bucket == "" || t.KeyRef == "" || d.KeyringID == "" || d.FormatVersion == 0 {
		return nil, errInternal()
	}
	now, err := clock.Now(ctx)
	if err != nil {
		return nil, err
	}
	run := []string{"sudo", "-u", "sitate", "sitate"}
	conn := []string{"--endpoint", t.Endpoint, "--bucket", t.Bucket, "--prefix", t.Prefix}
	ref := []string{"--key-ref", t.KeyRef, "--repo-id", d.RepoID}
	step := func(id string, argv ...string) map[string]any {
		if argv == nil {
			argv = []string{} // JSON の null にしない(スキーマは配列)
		}
		return map[string]any{"id": id, "argv": argv}
	}
	steps := []map[string]any{
		step("1"), step("2"),
		step("3", append(append(append(run, "storage", "issue-read-key"), conn...), append([]string{"--adapter", d.Provider, "--admin-ref", "admin"}, append(ref, "--run-id", "$RUN", "--secrets-stdin")...)...)...),
		step("4", append(append(append(run, "list"), conn...), append([]string{"--keyring", d.KeyringID}, append(ref, "--phrase-stdin")...)...)...),
		step("5", append(append(append(run, "restore"), conn...), append([]string{"--keyring", d.KeyringID}, append(ref, "--phrase-stdin", "--backup", "latest")...)...)...),
		step("6", append(append(run, "storage", "revoke-read-key"), "--endpoint", t.Endpoint, "--bucket", t.Bucket, "--prefix", t.Prefix, "--adapter", d.Provider, "--admin-ref", "admin", "--key-ref", t.KeyRef, "--secrets-stdin")...),
	}
	return map[string]any{
		"kit_version": d.version(), "issued_on": now.UTC().Format("2006-01-02"), "core_version": d.CoreVer, "format_version": d.FormatVersion,
		"distribution_url": d.DistURL,
		"core_sha256":      map[string]string{"linux_x86_64": d.CoreSHA["linux-x86_64"], "linux_arm64": d.CoreSHA["linux-arm64"]},
		"sbom_sha256":      d.SBOM, "source_sha256": d.Src, "signing_keys": d.SignKeys,
		"storage": map[string]any{"provider": d.Provider, "country": nil, "endpoint": t.Endpoint, "bucket": t.Bucket, "prefix": t.Prefix,
			"repo_id": d.RepoID, "keyring_id": d.KeyringID, "key_ref": t.KeyRef},
		"host_key_fp": d.HostFP, "bin_path": binPrefix(t.Prefix) + d.CoreVer + "/", "steps": steps,
		"provider_hints": map[string]string{"kind": d.Provider, "key_name": "sitate-" + t.Bucket + "-" + keyNameBase(t.Prefix) + "-read", "bin_prefix": binPrefix(t.Prefix)},
	}, nil
}
