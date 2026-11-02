package keys

// kit と kit confirm(spec 4.7)。kit は復旧キットの内容を構造化した値で出し(フレーズは含めない。出力は常に JSON)、文と印刷はアプリが行う。

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// hostKeyPubPath はバックアップサーバーの Ed25519 のホスト鍵(spec 4.7 の「管理用」の欄)。
const hostKeyPubPath = "/etc/ssh/ssh_host_ed25519_key.pub"

type kitBase struct {
	RepoID                        string
	KeyringID                     string
	KeyringVersion, FormatVersion int
}

// kitData は kit_version の計算と fields の組み立てに要る入力。
type kitData struct {
	kitBase
	Target     target
	SignFPs    []string
	SignKeys   []map[string]string
	HostFP     string
	CoreVer    string
	CoreSHA    map[string]string // "<os>-<arch>" → 16 進
	SBOM, Src  string
	DistURL    string
	Provider   string
	HaveBundle bool
}

// minisignFP は minisign の公開鍵 1 行の指紋(同じ鍵を OpenSSH の ed25519 の blob にして掛ける。spec 4.7)。
func minisignFP(line string) (string, bool) {
	a, ok := parseAnchor(line)
	if !ok {
		return "", false
	}
	var blob []byte
	for _, f := range [][]byte{[]byte("ssh-ed25519"), a.pub} {
		blob = binary.BigEndian.AppendUint32(blob, uint32(len(f)))
		blob = append(blob, f...)
	}
	return fingerprint(blob), true
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// hostKeyFP はホスト鍵の公開鍵ファイル(`ssh-ed25519 <base64> …`)の指紋。読めなければ空。
func hostKeyFP(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	f := strings.Fields(string(b))
	if len(f) < 2 || f[0] != "ssh-ed25519" {
		return ""
	}
	blob, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil {
		return ""
	}
	return fingerprint(blob)
}

func (k *Keys) hostKeyPath() string {
	if k.HostKeyPubPath != "" {
		return k.HostKeyPubPath
	}
	return hostKeyPubPath
}

func (k *Keys) kitData(ctx context.Context, base kitBase) (kitData, error) {
	d := kitData{kitBase: base, CoreSHA: map[string]string{}, HostFP: hostKeyFP(k.hostKeyPath()), Provider: "b2"}
	t, _, err := k.storageTarget(ctx, contract.Call{})
	if err != nil {
		return d, err
	}
	d.Target = t
	for _, line := range contract.TrustAnchors() {
		if fp, ok := minisignFP(line); ok {
			d.SignFPs = append(d.SignFPs, fp)
			d.SignKeys = append(d.SignKeys, map[string]string{"public_key": line, "fp": fp})
		}
	}
	slices.Sort(d.SignFPs)
	if k.ProvisionName != "" {
		d.Provider = k.ProvisionName
	}
	if k.Bundle == nil {
		return d, nil
	}
	ver, files, err := k.Bundle.Files(ctx, "")
	if err != nil {
		return d, err
	}
	d.CoreVer, d.HaveBundle = ver, true
	d.DistURL = strings.ReplaceAll(contract.DistURL, "<version>", ver)
	for _, f := range files {
		arch, rest, hasArch := strings.Cut(f.Path, "/")
		switch {
		case hasArch && rest == "sitate" && slices.Contains(distArches, arch):
			d.CoreSHA[arch] = sha256Hex(f.Data)
		case !hasArch && strings.HasSuffix(f.Path, ".spdx.json"):
			d.SBOM = sha256Hex(f.Data)
		case !hasArch && strings.HasSuffix(f.Path, "-src.tar.gz"):
			d.Src = sha256Hex(f.Data)
		}
	}
	return d, nil
}

// version は kit_version(spec 4.7「キットの版」の 11 行を連ねた SHA-256 の先頭 12 文字)。
func (d kitData) version() string {
	coreSHA := "linux-arm64:" + d.CoreSHA["linux-arm64"] + ",linux-x86_64:" + d.CoreSHA["linux-x86_64"]
	lines := []string{
		"repo_id=" + d.RepoID, "keyring_version=" + itoa(d.KeyringVersion), "endpoint=" + d.Target.Endpoint, "bucket=" + d.Target.Bucket, "prefix=" + d.Target.Prefix,
		"signing_key_fps=" + strings.Join(d.SignFPs, ","), "host_key_fp=" + d.HostFP, "core_version=" + d.CoreVer, "format_version=" + itoa(d.FormatVersion),
		"core_sha256=" + coreSHA, "key_ref=" + d.Target.KeyRef,
	}
	return sha256Hex([]byte(strings.Join(lines, "\n") + "\n"))[:12]
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func (k *Keys) kitVersion(ctx context.Context, base kitBase) (string, error) {
	d, err := k.kitData(ctx, base)
	if err != nil {
		return "", err
	}
	return d.version(), nil
}

// kitBaseNow は今の構成から kitBase を読む。リポジトリが無ければ NOT_INITIALIZED。
func (k *Keys) kitBaseNow(ctx context.Context) (kitBase, error) {
	var b kitBase
	if ok, err := k.getItem(ctx, "repo_id", &b.RepoID); err != nil {
		return b, err
	} else if !ok || k.Dir == "" {
		return b, mkErr(contract.CodeNotInitialized, empty{})
	}
	repo, err := readOpt(filepath.Join(k.Dir, contract.KeysRepoLatest))
	if err != nil {
		return b, errInternal()
	}
	b.KeyringVersion = 1
	if repo == nil { // init の直後(確定の前): 保留の repo.json とキーリングの版 1
		p, found, perr := readPending(k.Dir)
		if perr != nil || !found || p.Repo == nil {
			return b, mkErr(contract.CodeNotInitialized, empty{})
		}
		repo = p.Repo
	} else {
		var lpv struct {
			V int `json:"keyring_version"`
		}
		if _, err := k.getItem(ctx, "last_phrase_verify", &lpv); err != nil {
			return b, err
		}
		if lpv.V > 0 {
			b.KeyringVersion = lpv.V
		}
	}
	var rj struct {
		KeyringID     string `json:"keyring_id"`
		FormatVersion int    `json:"format_version"`
	}
	if json.Unmarshal(repo, &rj) != nil {
		return b, errInternal()
	}
	b.KeyringID, b.FormatVersion = rj.KeyringID, rj.FormatVersion
	return b, nil
}

// Kit は契約 contract.Keys を満たす。--format は json だけを受ける(pdf・printable は無い)。結果にフレーズを含めない。
func (k *Keys) Kit(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if f, ok := argOne(c, "format"); ok && f != "json" {
		return contract.Reply{}, errArg("--format", "unknown")
	}
	if err := errNeedDeps(k.State != nil, k.Clock != nil); err != nil {
		return contract.Reply{}, err
	}
	base, err := k.kitBaseNow(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	d, err := k.kitData(ctx, base)
	if err != nil {
		return contract.Reply{}, err
	}
	fields, err := d.fields(k.Clock, ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	kv := d.version()
	warns, err := k.syncKitVersion(ctx, kv)
	if err != nil {
		return contract.Reply{}, err
	}
	return reply(map[string]any{"kit_version": kv, "fields": fields}, warns...)
}

// syncKitVersion は状態ファイルの kit.version を今の構成の値にそろえ、確認済みの版と違えば KIT_OUTDATED を警告する。
func (k *Keys) syncKitVersion(ctx context.Context, kv string) ([]contract.Warning, error) {
	var kit map[string]any
	if _, err := k.getItem(ctx, "kit", &kit); err != nil {
		return nil, err
	}
	if kit == nil {
		kit = map[string]any{"confirmed_version": nil, "location_checked_at": nil, "confirmed_core_version": nil}
	}
	if kit["version"] != kv {
		kit["version"] = kv
		if err := k.setItems(ctx, "kit", kit); err != nil {
			return nil, err
		}
	}
	if cv, ok := kit["confirmed_version"].(string); ok && cv != kv {
		return []contract.Warning{mkWarn(contract.CodeKitOutdated, map[string]any{"kit_version": kv, "confirmed_kit_version": cv})}, nil
	}
	return nil, nil
}

// KitConfirm は 2 か所に保管したことの記録(--copies 2)。同じ版なら上書きしない。--recheck は呼ぶたびに location_checked_at を更新する。
func (k *Keys) KitConfirm(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := errNeedDeps(k.State != nil, k.Clock != nil); err != nil {
		return contract.Reply{}, err
	}
	ver, ok := argOne(c, "kit-version")
	if !ok || ver == "" {
		return contract.Reply{}, errArg("--kit-version", "missing")
	}
	if n, ok := argOne(c, "copies"); !ok || n != "2" {
		return contract.Reply{}, errArg("--copies", "format")
	}
	now, err := k.now(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	var out string
	err = k.withLock(ctx, func(h contract.StateLockHandle) error {
		var kit map[string]any
		if _, err := k.getItem(ctx, "kit", &kit); err != nil {
			return err
		}
		if kit == nil || kit["version"] != ver {
			return errArg("--kit-version", "mismatch")
		}
		if kit["confirmed_version"] != ver {
			kit["confirmed_version"], kit["location_checked_at"] = ver, fmtTime(now)
			if k.Bundle != nil {
				if cv, _, err := k.Bundle.Files(ctx, ""); err == nil && cv != "" {
					kit["confirmed_core_version"] = cv
				}
			}
		} else if argFlag(c, "recheck") {
			kit["location_checked_at"] = fmtTime(now)
		}
		b, _ := json.Marshal(kit)
		if err := k.State.Update(ctx, h, "kit", b); err != nil {
			return err
		}
		out, _ = kit["location_checked_at"].(string)
		return nil
	})
	if err != nil {
		return contract.Reply{}, err
	}
	return reply(map[string]any{"location_checked_at": out})
}
