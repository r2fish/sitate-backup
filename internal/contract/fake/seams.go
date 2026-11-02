package fake

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Bundle は contract.BundleSource の偽物。
type Bundle struct {
	Version string
	FilesBy map[string][]contract.BundleFile // osArch → ファイル。"" は全部
	Asked   []string                         // Files が受けた osArch(試験が復元先の arch で引いたかを見る)
}

// Files は contract.BundleSource を満たす。osArch が空なら全部、無い osArch は空。
func (b *Bundle) Files(_ context.Context, osArch string) (string, []contract.BundleFile, error) {
	b.Asked = append(b.Asked, osArch)
	if osArch != "" {
		return b.Version, b.FilesBy[osArch], nil
	}
	var all []contract.BundleFile
	for _, fs := range b.FilesBy {
		all = append(all, fs...)
	}
	return b.Version, all, nil
}

// OSProbe は contract.OSProbe の偽物。FS は FSType が返す種類(空なら "ext4")、Fail は Check が ok: false を返す項目。
type OSProbe struct {
	FS   string
	Fail map[string]bool
	Err  error
}

// FSType は contract.OSProbe を満たす。
func (o *OSProbe) FSType(context.Context, string) (string, error) {
	if o.Err != nil {
		return "", o.Err
	}
	if o.FS == "" {
		return "ext4", nil
	}
	return o.FS, nil
}

// Check は contract.OSProbe を満たす。
func (o *OSProbe) Check(_ context.Context, name string) (bool, json.RawMessage, error) {
	if o.Fail[name] {
		return false, json.RawMessage(`{}`), nil
	}
	return true, nil, nil
}
