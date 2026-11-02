package restore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type phraseVerify struct {
	KeyringVersion json.RawMessage `json:"keyring_version"`
	KeyringHash    string          `json:"keyring_hash"`
	KeyIDs         []string        `json:"key_ids"`
}

func (p phraseVerify) version() int {
	n, _ := strconv.Atoi(strings.Trim(string(p.KeyringVersion), `"`))
	return n
}

// latestKeyringObject は objects[] の keyring/ の最後の記録(キーリングの最新版を記録した版 ID)。
func latestKeyringObject(ctx context.Context, st contract.StateFiles) (objRec, bool, error) {
	var objs []objRec
	if _, err := item(ctx, st, "objects", &objs); err != nil {
		return objRec{}, false, err
	}
	for i := len(objs) - 1; i >= 0; i-- {
		if strings.HasPrefix(objs[i].Key, "keyring/") {
			return objs[i], true, nil
		}
	}
	return objRec{}, false, nil
}

// checkKeyring は spec 3.5 の表の 1: 保管先のキーリングの最新版を記録した版 ID で読み、そのハッシュが最後に phrase verify で開けた版と同じこと、
// 保持中のマニフェストが参照するマスター鍵の鍵 ID がすべてその版に含まれることを確かめる(違えば KEYRING_MISMATCH)。
func (t *rtState) checkKeyring(ctx context.Context) error {
	var lpv phraseVerify
	has, err := item(ctx, t.r.State, "last_phrase_verify", &lpv)
	if err != nil {
		return err
	}
	mismatch := func(reason string) error {
		return newErr(contract.CodeKeyringMismatch, map[string]any{"reason": reason, "keyring_version": lpv.version()})
	}
	obj, found, err := latestKeyringObject(ctx, t.r.State)
	if err != nil {
		return err
	}
	if !has || !found {
		return mismatch("hash")
	}
	b, err := t.o.readAll(ctx, obj.Key, obj.VersionID)
	if err != nil {
		return mismatch("hash")
	}
	h := sha256.Sum256(b)
	if contract.HashPrefix+hex.EncodeToString(h[:]) != lpv.KeyringHash {
		return mismatch("hash")
	}
	have := map[string]bool{}
	for _, k := range lpv.KeyIDs {
		have[k] = true
	}
	snaps, err := loadSnapshots(ctx, t.r.State)
	if err != nil {
		return err
	}
	for _, s := range snaps {
		if !have[t.manifestKeyID(ctx, s)] {
			return mismatch("key_id_missing")
		}
	}
	return nil
}

// checkConfig は spec 3.5 の表の 2: config/<def_hash> を記録した版 ID で読み、正規化のハッシュがマニフェストの def_hash と同じことを確かめる。
// 読めない・合わないときは警告 CONFIG_COPY_MISMATCH にして続ける(config_copy_ok: false)。
// configKey は config/ のキー。def_hash の値そのまま(sha256:<64 桁>)。ここ 1 か所で決める。
func configKey(defHash string) string { return "config/" + defHash }

func (t *rtState) checkConfig(ctx context.Context) bool {
	dh := t.man.DefHash
	cfgHash := any(nil)
	ok := false
	defer func() {
		if !ok {
			t.warnings = append(t.warnings, warn(contract.CodeConfigCopyMismatch, map[string]any{"config_def_hash": cfgHash, "manifest_def_hash": dh}))
		}
	}()
	obj, found, err := recordedObject(ctx, t.r.State, configKey(dh))
	if err != nil || !found {
		return false
	}
	sealed, err := t.o.readAll(ctx, obj.Key, obj.VersionID)
	if err != nil {
		return false
	}
	key, err := t.r.KeyS.DerivedKey("config")
	if err != nil {
		return false
	}
	plain, err := t.r.Codec.Decode(contract.MagicConfig, hexOfDefHash(dh), sealed, key)
	if err != nil {
		return false
	}
	var cfg struct {
		Normalized json.RawMessage `json:"normalized"`
	}
	if json.Unmarshal(plain, &cfg) != nil {
		return false
	}
	h, err := contract.CanonicalHash(cfg.Normalized)
	if err != nil {
		return false
	}
	cfgHash = h
	ok = h == dh
	return ok
}

// checkDefCopy は spec 3.5 の表の 9 の設定の再構成: マニフェストの定義の写しの def_hash が一致し、定義の検査(参照名の解決を除く)に通ること。
// 欄が無い・null は読めない写しとして不一致にする。合わないときは警告 DEF_COPY_MISMATCH にして合格のままにする。
func (t *rtState) checkDefCopy(ctx context.Context) *bool {
	f := false
	bad := func(reason string) *bool {
		t.warnings = append(t.warnings, warn(contract.CodeDefCopyMismatch, map[string]any{"backup_ref": t.snap.Ref, "reason": reason}))
		return &f
	}
	if len(t.man.Definition) == 0 || string(t.man.Definition) == "null" {
		return bad("unreadable") // スキーマで必須。無いのは異常
	}
	h, err := contract.CanonicalHash(t.man.Definition)
	if err != nil {
		return bad("unreadable")
	}
	if h != t.man.DefHash {
		return bad("hash_mismatch")
	}
	_, _, issues, err := t.r.State.ValidateDef(ctx, t.man.Definition, true)
	var iss []json.RawMessage
	if err != nil || (len(issues) > 0 && json.Unmarshal(issues, &iss) == nil && len(iss) > 0) {
		return bad("invalid")
	}
	tr := true
	return &tr
}

// headerBytes はオブジェクトの先頭 n バイトだけを読んで閉じる(見出しの分。読んだ量は核の側の読み出しに数える)。
func (o *objReader) headerBytes(ctx context.Context, key, versionID string, n int) ([]byte, error) {
	rc, err := o.store.Get(ctx, key, versionID)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	buf := make([]byte, n)
	got, err := io.ReadFull(rc, buf)
	o.read += int64(got)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:got], nil
}

// manifestKeyID は世代のマニフェストが参照するマスター鍵の鍵 ID。見出しの印だけで決め、本体は読まない・復号しない
// (キーリングに無い鍵の世代は復号できないので、復号の成否に頼ると見逃す。T-20)。見出しが読めなければ空(= キーリングに含まれない扱い)。
// 見出しに印が無い Codec(偽物)のときだけ、本体を開いて鍵 ID を読む。
// 世代の一覧に鍵 ID を持たせる案(snapshots[].key_id)が通れば、この関数だけを差し替える。
func (t *rtState) manifestKeyID(ctx context.Context, s snapRec) string {
	b, err := t.o.headerBytes(ctx, "manifests/"+s.Ref, s.VersionID, manifestHeaderMax)
	if err != nil {
		return ""
	}
	h, err := t.r.Codec.Header(b)
	if err != nil {
		return ""
	}
	if id := keyIDFromMark(h.Mark); id != "" {
		return id
	}
	m, _, err := t.o.manifest(ctx, s.Ref, s.VersionID, nil)
	if err != nil {
		return ""
	}
	return m.KeyID
}

// manifestHeaderMax はマニフェストの見出し(種類 4 + 版 1 + 鍵 ID 4 + 包んだ鍵 64 + ノンス 24)を含む読み出しの上限。
const manifestHeaderMax = 256
