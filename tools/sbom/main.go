// Command sbom は SPDX 2.3 の JSON の SBOM を出す(作業指示書 3.1 の make build)。
// 対象は git の管理下のファイル(git ls-files。HEAD が無いときは未追跡の非 ignore も含める)と、Go のモジュールの依存。
package main

import (
	"crypto/sha1" //nolint:gosec // SPDX 2.3 の checksum は SHA1 を必須とする
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type checksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"checksumValue"`
}

type file struct {
	ID        string     `json:"SPDXID"`
	Name      string     `json:"fileName"`
	Checksums []checksum `json:"checksums"`
	License   string     `json:"licenseConcluded"`
	Copyright string     `json:"copyrightText"`
}

type pkg struct {
	ID       string `json:"SPDXID"`
	Name     string `json:"name"`
	Version  string `json:"versionInfo,omitempty"`
	Download string `json:"downloadLocation"`
	Files    bool   `json:"filesAnalyzed"`
	License  string `json:"licenseConcluded"`
	Declared string `json:"licenseDeclared"`
	Copy     string `json:"copyrightText"`
}

type rel struct {
	Element string `json:"spdxElementId"`
	Type    string `json:"relationshipType"`
	Related string `json:"relatedSpdxElement"`
}

func sh(name string, args ...string) []string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sbom:", name, err)
		os.Exit(1)
	}
	var r []string
	for _, l := range strings.Split(string(out), "\n") {
		if l != "" {
			r = append(r, l)
		}
	}
	return r
}

func main() {
	version, out := os.Args[1], os.Args[2]
	var names []string
	if exec.Command("git", "rev-parse", "--verify", "-q", "HEAD").Run() != nil {
		names = sh("git", "ls-files", "--cached", "--others", "--exclude-standard")
	} else {
		names = sh("git", "ls-files")
	}
	var files []file
	var rels []rel
	for i, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			continue
		}
		s1, s2 := sha1.Sum(b), sha256.Sum256(b)
		id := fmt.Sprintf("SPDXRef-File-%d", i)
		files = append(files, file{id, "./" + n, []checksum{{"SHA1", hex.EncodeToString(s1[:])}, {"SHA256", hex.EncodeToString(s2[:])}}, "NOASSERTION", "NOASSERTION"})
		rels = append(rels, rel{"SPDXRef-Package-sitate-backup", "CONTAINS", id})
	}
	pkgs := []pkg{{"SPDXRef-Package-sitate-backup", "sitate-backup", version, "NOASSERTION", false, "NOASSERTION", "NOASSERTION", "NOASSERTION"}}
	for i, m := range sh("go", "list", "-m", "-f", "{{if not .Main}}{{.Path}} {{.Version}}{{end}}", "all") {
		f := strings.Fields(m)
		if len(f) != 2 {
			continue
		}
		id := fmt.Sprintf("SPDXRef-Package-dep-%d", i)
		pkgs = append(pkgs, pkg{id, f[0], f[1], "https://proxy.golang.org/" + f[0] + "/@v/" + f[1] + ".zip", false, "NOASSERTION", "NOASSERTION", "NOASSERTION"})
		rels = append(rels, rel{"SPDXRef-Package-sitate-backup", "DEPENDS_ON", id})
	}
	doc := map[string]any{
		"spdxVersion": "SPDX-2.3", "dataLicense": "CC0-1.0", "SPDXID": "SPDXRef-DOCUMENT",
		"name": "sitate-backup-" + version, "documentNamespace": "https://sitate.invalid/spdx/sitate-backup-" + version,
		"creationInfo": map[string]any{"created": time.Now().UTC().Format("2006-01-02T15:04:05Z"), "creators": []string{"Tool: sitate-sbom"}},
		"packages":     pkgs, "files": files, "relationships": rels,
	}
	b, _ := json.MarshalIndent(doc, "", " ")
	if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
