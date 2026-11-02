package keys

// bin/ に写す配布物の選別と検証(spec 4.2「bin/ の中身と版の記録」、3.2.1)。tools/build.sh・tools/sign.sh の出力に合わせる:
//   <arch>/sitate(署名つき)、<arch>/adapters/provision/<name>/adapter.json(署名つき)と、adapter.json の exe が指す実行ファイル(署名なし。
//   検証済みの adapter.json の sha256 と照らす)、版ディレクトリ直下の *-src.tar.gz と *.spdx.json(署名つき。arch は any)。
// 両 arch の核と提供アダプタ、ソース、SBOM がそろわなければ何も書かずに失敗する。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const (
	anyArch   = "any"
	provPref  = "adapters/provision/"
	srcSuffix = "-src.tar.gz"
	sbomSufix = ".spdx.json"
)

var distArches = []string{"linux-x86_64", "linux-arm64"}

// binItem は bin/ に写す 1 ファイル。署名の無い実行ファイルは Sig が nil。
type binItem struct {
	path string
	data []byte
	sig  []byte
}

func badSig(reason string) error {
	return mkErr(contract.CodeSignatureInvalid, map[string]any{"reason": reason, "skew_s": nil})
}

// verifySigned は期待値(版・arch・相対パス)を自分で作って署名を検証する(配布物の Want は使わない)。
func (k *Keys) verifySigned(ctx context.Context, version, arch string, f contract.BundleFile) (binItem, error) {
	ok, err := k.Verifier.Verify(ctx, f.Data, f.Sig, contract.TrustedComment{Version: version, Arch: arch, Path: f.Path})
	if err != nil {
		return binItem{}, err
	}
	if !ok {
		return binItem{}, badSig("bad_signature")
	}
	return binItem{path: f.Path, data: f.Data, sig: f.Sig}, nil
}

func (k *Keys) binFiles(ctx context.Context) (version string, items []binItem, err error) {
	version, files, err := k.Bundle.Files(ctx, "")
	if err != nil {
		return "", nil, err
	}
	if version == "" {
		return "", nil, errInternal()
	}
	byPath := map[string]contract.BundleFile{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	for _, arch := range distArches {
		core, ok := byPath[arch+"/sitate"]
		if !ok {
			return "", nil, errInternal()
		}
		it, err := k.verifySigned(ctx, version, arch, core)
		if err != nil {
			return "", nil, err
		}
		items = append(items, it)
		ad, err := k.provisionItems(ctx, version, arch, files, byPath)
		if err != nil {
			return "", nil, err
		}
		items = append(items, ad...)
	}
	for _, suf := range []string{srcSuffix, sbomSufix} {
		var found []contract.BundleFile
		for _, f := range files {
			if !strings.Contains(f.Path, "/") && strings.HasSuffix(f.Path, suf) {
				found = append(found, f)
			}
		}
		if len(found) != 1 {
			return "", nil, errInternal()
		}
		it, err := k.verifySigned(ctx, version, anyArch, found[0])
		if err != nil {
			return "", nil, err
		}
		items = append(items, it)
	}
	return version, items, nil
}

// provisionItems は arch の保管先準備アダプタ(1 つ以上)を検証して返す。実行ファイルは検証済みの adapter.json の sha256 と照らす。
func (k *Keys) provisionItems(ctx context.Context, version, arch string, files []contract.BundleFile, byPath map[string]contract.BundleFile) ([]binItem, error) {
	var items []binItem
	for _, f := range files {
		rest, ok := strings.CutPrefix(f.Path, arch+"/"+provPref)
		if !ok || path.Base(rest) != "adapter.json" || strings.Count(rest, "/") != 1 {
			continue
		}
		it, err := k.verifySigned(ctx, version, arch, f)
		if err != nil {
			return nil, err
		}
		var aj struct {
			Exe    string `json:"exe"`
			SHA256 string `json:"sha256"`
		}
		if json.Unmarshal(f.Data, &aj) != nil || aj.Exe == "" || strings.ContainsAny(aj.Exe, "/\x00") {
			return nil, errInternal()
		}
		exe, ok := byPath[path.Dir(f.Path)+"/"+aj.Exe]
		if !ok {
			return nil, errInternal()
		}
		sum := sha256.Sum256(exe.Data)
		if want := strings.TrimPrefix(strings.ToLower(aj.SHA256), "sha256:"); want != hex.EncodeToString(sum[:]) {
			return nil, badSig("hash_mismatch")
		}
		items = append(items, it, binItem{path: exe.Path, data: exe.Data})
	}
	if len(items) == 0 {
		return nil, errInternal()
	}
	return items, nil
}

// binItems は bin/ に書くものを検証済みの形で並べる(署名のあるものは .sig も)。
func (k *Keys) binItems(ctx context.Context) ([]putItem, error) {
	version, bins, err := k.binFiles(ctx)
	if err != nil {
		return nil, err
	}
	var out []putItem
	for _, b := range bins {
		key := "bin/" + version + "/" + b.path
		out = append(out, putItem{key, b.data})
		if b.sig != nil {
			out = append(out, putItem{key + ".sig", b.sig})
		}
	}
	return out, nil
}
