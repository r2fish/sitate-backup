package cli

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// BundleDirDefault は配布物の置き場(spec 4.2。`bootstrap` が `bundle/<version>/` を置く。アプリの同梱の物も同じ並び)。
const BundleDirDefault = "/usr/libexec/sitate/bundle"

// BundleAnyArch は <os>-<arch> の下に無いファイル(価格表・ソース・SBOM)の trusted comment の arch(spec 3.2.1)。
const BundleAnyArch = "any"

// DirBundles は contract.BundleSource の実物: 配布物のディレクトリ <Root>/<version>/ を読む。保管先の bin/ からは読まない(spec 3.5 の表の 6)。
// 版は Version(動いている核の版)の名前のディレクトリ。無ければ、版ディレクトリがちょうど 1 つのときそれを使う。
type DirBundles struct {
	Root    string // 空なら BundleDirDefault
	Version string // 空なら Version(ビルドの版)
}

var _ contract.BundleSource = (*DirBundles)(nil)

// Files は osArch("<os>-<arch>"。空なら全部)のファイルを返す。署名ファイル <名前>.sig は対象の Sig に入れ、単独では返さない。
// 署名が無いファイルも Sig を空で返す(検証の側が落とす)。Want は版・arch(<os>-<arch> の下ならその名前、それ以外は any)・版ディレクトリからの相対パス。
func (b *DirBundles) Files(ctx context.Context, osArch string) (string, []contract.BundleFile, error) {
	root := b.Root
	if root == "" {
		root = BundleDirDefault
	}
	ver, err := b.pickVersion(root)
	if err != nil {
		return "", nil, err
	}
	base := filepath.Join(root, ver)
	var out []contract.BundleFile
	err = filepath.WalkDir(base, func(p string, e fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.IsDir() || !e.Type().IsRegular() || strings.HasSuffix(p, ".sig") {
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		rel = filepath.ToSlash(rel)
		arch := BundleAnyArch
		if i := strings.IndexByte(rel, '/'); i > 0 && isOSArch(rel[:i]) {
			arch = rel[:i]
		}
		if osArch != "" && arch != BundleAnyArch && arch != osArch {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sig, err := os.ReadFile(p + ".sig")
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		out = append(out, contract.BundleFile{Path: rel, Data: data, Sig: sig, Want: contract.TrustedComment{Version: ver, Arch: arch, Path: rel}})
		return nil
	})
	if err != nil {
		return "", nil, internalErr()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return ver, out, nil
}

func (b *DirBundles) pickVersion(root string) (string, error) {
	want := b.Version
	if want == "" {
		want = Version
	}
	if fi, err := os.Stat(filepath.Join(root, want)); err == nil && fi.IsDir() {
		return want, nil
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return "", internalErr()
	}
	var dirs []string
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) != 1 {
		return "", internalErr()
	}
	return dirs[0], nil
}

// isOSArch は "linux-x86_64"・"linux-arm64" の形(<os>-<arch>)か。
func isOSArch(s string) bool {
	i := strings.IndexByte(s, '-')
	return i > 0 && i < len(s)-1 && !strings.ContainsAny(s, "/.")
}
