package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func writeBundle(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		f := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDirBundlesFiles(t *testing.T) {
	root := t.TempDir()
	writeBundle(t, root, map[string]string{
		"1.2.3/linux-x86_64/sitate": "x86", "1.2.3/linux-x86_64/sitate.sig": "sx",
		"1.2.3/linux-arm64/sitate": "arm", "1.2.3/linux-arm64/sitate.sig": "sa",
		"1.2.3/prices/prices.json": "p", "1.2.3/prices/prices.json.sig": "sp",
		"1.2.3/sitate-1.2.3.spdx.json": "sbom",
	})
	b := &DirBundles{Root: root, Version: "1.2.3"}
	ver, fs, err := b.Files(context.Background(), "linux-x86_64")
	if err != nil || ver != "1.2.3" {
		t.Fatalf("ver=%q err=%v", ver, err)
	}
	got := map[string]contract.BundleFile{}
	for _, f := range fs {
		got[f.Path] = f
	}
	if len(got) != 3 {
		t.Fatalf("arch で絞れていない: %v", got)
	}
	x := got["linux-x86_64/sitate"]
	if string(x.Data) != "x86" || string(x.Sig) != "sx" || x.Want != (contract.TrustedComment{Version: "1.2.3", Arch: "linux-x86_64", Path: "linux-x86_64/sitate"}) {
		t.Errorf("核: %+v", x)
	}
	if w := got["prices/prices.json"].Want; w.Arch != "any" || w.Path != "prices/prices.json" {
		t.Errorf("価格表の Want: %+v", w)
	}
	if got["sitate-1.2.3.spdx.json"].Sig != nil {
		t.Error("署名の無いファイルの Sig は空")
	}
	_, all, _ := b.Files(context.Background(), "")
	if len(all) != 4 {
		t.Errorf("全部: %d", len(all))
	}
}

func TestDirBundlesVersionPick(t *testing.T) {
	root := t.TempDir()
	writeBundle(t, root, map[string]string{"9.9.9/prices/prices.json": "p"})
	if v, _, err := (&DirBundles{Root: root, Version: "0.0.1"}).Files(context.Background(), ""); err != nil || v != "9.9.9" {
		t.Errorf("版が 1 つだけならそれを使う: %q %v", v, err)
	}
	writeBundle(t, root, map[string]string{"8.8.8/prices/prices.json": "p"})
	if _, _, err := (&DirBundles{Root: root, Version: "0.0.1"}).Files(context.Background(), ""); err == nil {
		t.Error("版が曖昧なのに成功した")
	}
	if _, _, err := (&DirBundles{Root: filepath.Join(root, "none")}).Files(context.Background(), ""); err == nil {
		t.Error("置き場が無いのに成功した")
	}
}
