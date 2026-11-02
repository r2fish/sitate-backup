package restore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// destKey は --snapshot-key-stdin の 1 行目(DestKeyLine)を読んだもの。
type destKey struct {
	snap          []byte
	keyID, secret string
}

func parseDestKey(s *contract.Secret) (destKey, error) {
	var l contract.DestKeyLine
	if s == nil || json.Unmarshal(s.Reveal(), &l) != nil {
		return destKey{}, argInvalid("--snapshot-key-stdin", "format")
	}
	k, err := base64.StdEncoding.DecodeString(l.SnapshotKey)
	if err != nil || len(k) == 0 || l.ReadKey.KeyID == "" || l.ReadKey.Secret == "" {
		return destKey{}, argInvalid("--snapshot-key-stdin", "format")
	}
	return destKey{snap: k, keyID: l.ReadKey.KeyID, secret: l.ReadKey.Secret}, nil
}

// checkEmptyDir は --to が存在しないか空のディレクトリであること(spec 3.1.2 の表。書き込み先は空のディレクトリに限る)。
func checkEmptyDir(to string) error {
	fi, err := os.Lstat(to)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !fi.IsDir() {
		return argInvalid("--to", "mismatch")
	}
	des, err := os.ReadDir(to)
	if err != nil || len(des) > 0 {
		return argInvalid("--to", "mismatch")
	}
	return nil
}

// resolveManifest は --backup(ref か latest / latest-any)を、保管先の版一覧から復号できる版のマニフェストにする。
// 版を新しい順に、スナップショット鍵で認証付き復号に成功するまで試す(書き込み鍵で積まれた偽の版で止まらない。spec 4.2)。
// latest は健常性 pass かつ全対象 ok のものだけ。当たる世代が無ければ latest は NO_HEALTHY_BACKUP、ref は最後の復号の失敗。
func resolveManifest(ctx context.Context, o *objReader, backup string, snapKey []byte) (string, *manifestDoc, error) {
	vers, err := o.store.ListVersions(ctx, "manifests/")
	if err != nil {
		return "", nil, err
	}
	var cands []contract.VersionInfo
	for _, v := range vers {
		if !v.DeleteMarker && strings.HasPrefix(v.Key, "manifests/") {
			ref := strings.TrimPrefix(v.Key, "manifests/")
			if backup == "latest" || backup == "latest-any" || ref == backup {
				cands = append(cands, v)
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].LastModified.After(cands[j].LastModified) })
	last := decryptErr("manifest", "manifests/"+backup, "", "", contract.ErrMissing)
	for _, v := range cands {
		ref := strings.TrimPrefix(v.Key, "manifests/")
		m, _, err := o.manifest(ctx, ref, v.VersionID, snapKey)
		if err != nil {
			if ctx.Err() != nil {
				return "", nil, abortError(ctx)
			}
			last = err
			continue
		}
		if backup == "latest" && !(m.Health == "pass" && allTargetsOK(m)) {
			continue
		}
		return ref, m, nil
	}
	if backup == "latest" || backup == "latest-any" {
		return "", nil, newErr(contract.CodeNoHealthyBackup, nil)
	}
	return "", nil, last
}

func allTargetsOK(m *manifestDoc) bool {
	for _, t := range m.Targets {
		if t.Status != "ok" {
			return false
		}
	}
	return true
}

// Restore は (a) その場のディレクトリへの復元(spec 3.1.2.4)。引数の保管先を標準入力の読み取り鍵で開き、
// スナップショット鍵でマニフェストを開いて、エントリを書き出し(materialize)、書いたものを RestoreVerify で照合する。
func (r *Restore) Restore(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if r == nil || r.Codec == nil || r.KeyS == nil || r.Crypto == nil || r.OpenStore == nil || r.Clock == nil {
		return contract.Reply{}, internalErr("")
	}
	if c.DryRun {
		return contract.Reply{}, contract.ErrNotImplemented("restore.Restore.dry-run")
	}
	to, err := filepath.Abs(arg(c, contract.ArgTo))
	if arg(c, contract.ArgTo) == "" || err != nil {
		return contract.Reply{}, argInvalid("--to", "missing")
	}
	addr := contract.StoreAddr{Endpoint: arg(c, contract.ArgEndpoint), Bucket: arg(c, contract.ArgBucket), Prefix: arg(c, contract.ArgPrefix)}
	if addr.Endpoint == "" || addr.Bucket == "" {
		return contract.Reply{}, argInvalid("--endpoint", "missing")
	}
	backup := arg(c, contract.ArgBackup)
	if backup == "" {
		return contract.Reply{}, argInvalid("--backup", "missing")
	}
	if c.SnapshotKeyIn == nil {
		return contract.Reply{}, argInvalid("--snapshot-key-stdin", "missing")
	}
	if err := checkEmptyDir(to); err != nil {
		return contract.Reply{}, err
	}
	dk, err := parseDestKey(c.SnapshotKeyIn)
	if err != nil {
		return contract.Reply{}, err
	}
	sec := contract.NewSecret([]byte(dk.secret))
	defer sec.Wipe()
	store, err := r.OpenStore(ctx, addr, dk.keyID, sec)
	if err != nil {
		return contract.Reply{}, err
	}
	return r.restoreInto(ctx, store, to, backup, dk.snap)
}

func (r *Restore) restoreInto(ctx context.Context, store contract.ObjectStore, to, backup string, snapKey []byte) (contract.Reply, error) {
	start, _ := r.Clock.Now(ctx)
	o := &objReader{store: store, codec: r.Codec, keyS: r.KeyS}
	ref, m, err := resolveManifest(ctx, o, backup, snapKey)
	if err != nil {
		return contract.Reply{}, err
	}
	entries, err := o.loadEntries(ctx, m)
	if err != nil {
		return contract.Reply{}, err
	}
	rows, err := o.keyTable(ctx, m)
	if err != nil {
		return contract.Reply{}, err
	}
	src := newChunkSource(ctx, o, rows)
	mat, err := materialize(ctx, to, entries, src.content, r.hashStream)
	if err != nil {
		if ctx.Err() != nil {
			return contract.Reply{}, abortError(ctx)
		}
		return contract.Reply{}, err
	}
	ok, files, bytes, _, mm := verifyFiles(expectedFiles(entries), mat.actual)
	if len(mat.failures) > 0 || !ok {
		return contract.Reply{}, mismatchErr(mat, entries, mm)
	}
	end, _ := r.Clock.Now(ctx)
	health := m.Health
	if health == "" {
		health = "unknown"
	}
	return reply(map[string]any{"backup_ref": ref, "health": health, "files": files, "bytes": bytes, "hash_mismatch": 0,
		"checks": []any{}, "duration_s": int64(end.Sub(start).Seconds()), "to": to, "read_bytes": o.read})
}

// hashStream は Codec.HashStream の値を "blake3:" 付きにそろえる。
func (r *Restore) hashStream(rd io.Reader) (string, error) {
	h, err := r.Codec.HashStream(rd)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(h, "blake3:") {
		h = "blake3:" + h
	}
	return h, nil
}

func isHashed(e entry) bool { return e.Type == "file" || e.Type == "dump" }

// expectedFiles はマニフェストのエントリから作る照合の期待(ファイルとダンプ)。
func expectedFiles(entries []entry) []contract.RestoreVerifyFile {
	var out []contract.RestoreVerifyFile
	for _, e := range entries {
		if isHashed(e) {
			out = append(out, contract.RestoreVerifyFile{EntryRef: e.Ref, Size: e.Size, Hash: e.Hash})
		}
	}
	return out
}

// mismatchErr は RESTORE_MISMATCH(spec 3.1.2.4 の結果)。hash_mismatch[] はチャンク単位で、refs[] は影響したエントリ。
// files_failed は置けなかったファイルと照合の不一致(欠け・余分を含む)の和集合で数える。
func mismatchErr(mat *materializer, entries []entry, mm []mismatch) error {
	type hm struct {
		ChunkID   string   `json:"chunk_id"`
		VersionID string   `json:"version_id"`
		Reason    string   `json:"reason"`
		Refs      []string `json:"refs"`
	}
	idx := map[string]*hm{}
	list := []*hm{}
	failed := map[string]bool{}
	for i, f := range mat.failures {
		for _, ref := range mat.failRefs[i] {
			k := f.ChunkID + "\x00" + f.VersionID + "\x00" + f.Reason
			h, ok := idx[k]
			if !ok {
				h = &hm{ChunkID: f.ChunkID, VersionID: f.VersionID, Reason: f.Reason, Refs: []string{}}
				idx[k] = h
				list = append(list, h)
			}
			h.Refs = append(h.Refs, ref)
			failed[ref] = true
		}
	}
	for _, x := range mm {
		failed[x.EntryRef] = true
	}
	total := len(expectedFiles(entries))
	return newErr(contract.CodeRestoreMismatch, map[string]any{"hash_mismatch": list, "files_ok": max(total-len(failed), 0), "files_failed": len(failed)})
}
