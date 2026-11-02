package keys

// phrase verify(spec 3.1.2、4.3「保留と確定」「確定の書く順序と途中の失敗」、4.2「bin/ の中身と版の記録」)。
// 保留中のキーリングがフレーズで開けたときだけ確定する: repo.json → キーリング → bin/ の順に、書き込み予定を実行の記録に残してから書き、
// 版 ID を受けるたびに状態ファイルに記録する。全部書いてから保留を消す。開けないフレーズは何も書かず、保留を残す。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type objRec struct {
	Key         string `json:"key"`
	VersionID   string `json:"version_id"`
	Size        int64  `json:"size"`
	ETag        string `json:"etag"`
	RetainUntil string `json:"retain_until"`
	WrittenAt   string `json:"written_at"`
}

type putItem struct {
	key  string
	body []byte
}

// PhraseVerify は契約 contract.Keys を満たす。
func (k *Keys) PhraseVerify(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := errNeedDeps(k.State != nil, k.KeyS != nil, k.Store != nil, k.Recorder != nil, k.Clock != nil, k.Verifier != nil, k.Bundle != nil, k.Dir != ""); err != nil {
		return contract.Reply{}, err
	}
	if c.PhraseIn == nil {
		return contract.Reply{}, errArg("--phrase-stdin", "missing")
	}
	defer c.PhraseIn.Wipe()
	pend, found, err := readPending(k.Dir)
	if err != nil {
		return contract.Reply{}, errInternal()
	}
	if found {
		return k.commitVerify(ctx, c.PhraseIn, pend)
	}
	return k.reverify(ctx, c.PhraseIn)
}

// reverify は確定済み(または保留の無い)リポジトリの往復検査: keys/keyring.latest を開けるかを確かめ、保管先へは書かない。
func (k *Keys) reverify(ctx context.Context, phrase *contract.Secret) (contract.Reply, error) {
	kr, err := readOpt(filepath.Join(k.Dir, contract.KeysKeyringLatest))
	if err != nil {
		return contract.Reply{}, errInternal()
	}
	if kr == nil {
		return contract.Reply{}, mkErr(contract.CodeNotInitialized, empty{})
	}
	info, err := k.KeyS.OpenedKeyring(ctx, phrase, kr)
	if err != nil {
		return contract.Reply{}, err
	}
	var lpv struct {
		VersionID string `json:"keyring_version_id"`
	}
	if _, err := k.getItem(ctx, "last_phrase_verify", &lpv); err != nil {
		return contract.Reply{}, err
	}
	if lpv.VersionID == "" {
		return contract.Reply{}, errInternal()
	}
	if err := k.setItems(ctx, "phrase_confirmed", true); err != nil {
		return contract.Reply{}, err
	}
	return reply(map[string]any{"verified": true, "keyring_version": info.Version, "keyring_version_id": lpv.VersionID, "committed": false})
}

// commitVerify は保留を開き、開けたら残りを書いて確定する。
func (k *Keys) commitVerify(ctx context.Context, phrase *contract.Secret, pend pendingFiles) (contract.Reply, error) {
	info, err := k.KeyS.OpenedKeyring(ctx, phrase, pend.Keyring)
	if err != nil {
		return contract.Reply{}, err
	}
	var repo struct {
		KeyringID string `json:"keyring_id"`
	}
	if pend.Repo != nil && json.Unmarshal(pend.Repo, &repo) != nil {
		return contract.Reply{}, errInternal()
	}
	if repo.KeyringID == "" {
		repo.KeyringID = info.KeyringID
	}
	var items []putItem
	if pend.Repo != nil {
		items = append(items, putItem{"repo.json", pend.Repo})
	}
	items = append(items, putItem{"keyring/" + repo.KeyringID, pend.Keyring})
	switch pend.Op {
	case "init":
		bin, err := k.binItems(ctx)
		if err != nil {
			return contract.Reply{}, err
		}
		items = append(items, bin...)
	case "phrase-reissue", "key-rotate": // キーリングの新しい版だけを書く
	default:
		return contract.Reply{}, errInternal()
	}
	recs, err := k.writeAll(ctx, items)
	if err != nil {
		return contract.Reply{}, err
	}
	kvid := ""
	for _, r := range recs {
		if r.Key == "keyring/"+repo.KeyringID {
			kvid = r.VersionID
		}
	}
	now, err := k.now(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	sum := sha256.Sum256(pend.Keyring)
	// 確定のファイルを置く → phrase_confirmed を書く → 保留を消す(ファイルが置けなかったのに確定済みと見えないように)。
	if err := placeCommitted(k.Dir, pend); err != nil {
		return contract.Reply{}, errInternal()
	}
	err = k.setItems(ctx, "last_phrase_verify", map[string]any{
		"keyring_version": info.Version, "keyring_version_id": kvid, "keyring_hash": "sha256:" + hex.EncodeToString(sum[:]),
		"key_ids": info.KeyIDs, "at": fmtTime(now)}, "phrase_confirmed", true)
	if err != nil {
		return contract.Reply{}, err
	}
	if err := removePending(k.Dir); err != nil {
		return contract.Reply{}, errInternal()
	}
	return reply(map[string]any{"verified": true, "keyring_version": info.Version, "keyring_version_id": kvid, "committed": true})
}

// writeAll は items を順に書く。状態ファイルに版 ID を記録した物は書かずに残りを書く(再実行)。書くたびに書き込み予定を残し、版 ID を記録する。
func (k *Keys) writeAll(ctx context.Context, items []putItem) ([]objRec, error) {
	var recs []objRec
	if _, err := k.getItem(ctx, "objects", &recs); err != nil {
		return nil, err
	}
	have := map[string]objRec{}
	for _, r := range recs {
		have[r.Key] = r
	}
	var out []objRec
	for _, it := range items {
		if r, ok := have[it.key]; ok {
			out = append(out, r)
			continue
		}
		if err := k.recordIntent(ctx, it.key); err != nil {
			return nil, err
		}
		res, err := k.Store.Put(ctx, it.key, bytes.NewReader(it.body), contract.RetentionDaily)
		if err != nil {
			return nil, err
		}
		now, err := k.now(ctx)
		if err != nil {
			return nil, err
		}
		r := objRec{Key: it.key, VersionID: res.VersionID, Size: res.Size, ETag: res.ETag, RetainUntil: fmtTime(res.RetainUntil), WrittenAt: fmtTime(now)}
		recs = append(recs, r)
		if err := k.setItems(ctx, "objects", recs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
