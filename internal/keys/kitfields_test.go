package keys

import (
	"slices"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

// kit の fields の steps は spec 4.7 の手順 (1)〜(6) の字面に合う(手順 (5) の --to は必須)。
func TestKitFieldsSteps(t *testing.T) {
	h64 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	d := kitData{
		kitBase:    kitBase{RepoID: "r-1", KeyringID: "k-1", KeyringVersion: 1, FormatVersion: 1},
		Target:     target{Endpoint: "https://s3.example.invalid", Bucket: "bkt", Prefix: "p/", KeyRef: "ref1"},
		SignKeys:   []map[string]string{{"public_key": "x", "fp": "SHA256:x"}},
		HostFP:     "SHA256:h",
		CoreVer:    "1.0.0",
		CoreSHA:    map[string]string{"linux-x86_64": h64, "linux-arm64": h64},
		SBOM:       h64,
		Src:        h64,
		Provider:   "b2",
		HaveBundle: true,
	}
	f, err := d.fields(&fake.Clock{}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	steps := f["steps"].([]map[string]any)
	conn := []string{"--endpoint", "https://s3.example.invalid", "--bucket", "bkt", "--prefix", "p/"}
	run := []string{"sudo", "-u", "sitate", "sitate"}
	ref := []string{"--key-ref", "ref1", "--repo-id", "r-1"}
	cat := func(parts ...[]string) []string {
		var o []string
		for _, p := range parts {
			o = append(o, p...)
		}
		return o
	}
	want := [][]string{
		{}, {},
		cat(run, []string{"storage", "issue-read-key"}, conn, []string{"--adapter", "b2", "--admin-ref", "admin"}, ref, []string{"--run-id", "$RUN", "--secrets-stdin"}),
		cat(run, []string{"list"}, conn, []string{"--keyring", "k-1"}, ref, []string{"--phrase-stdin"}),
		cat(run, []string{"restore"}, conn, []string{"--keyring", "k-1"}, ref, []string{"--phrase-stdin", "--backup", "latest", "--to", "/var/lib/sitate/restore/$RUN/to"}),
		cat(run, []string{"storage", "revoke-read-key"}, conn, []string{"--adapter", "b2", "--admin-ref", "admin", "--key-ref", "ref1", "--secrets-stdin"}),
	}
	if len(steps) != len(want) {
		t.Fatalf("steps の数 %d", len(steps))
	}
	for i, w := range want {
		if steps[i]["id"] != string(rune('1'+i)) {
			t.Errorf("steps[%d].id = %v", i, steps[i]["id"])
		}
		if got := steps[i]["argv"].([]string); !slices.Equal(got, w) {
			t.Errorf("手順 (%d) の argv\n got %q\nwant %q", i+1, got, w)
		}
	}
}
