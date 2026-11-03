package acquire

// backup の取得の入口(spec 7.5 の backup の流れ 1): 定義の対象ごとに取得アダプタを呼び、ストリームを照合して保管し、
// エントリと鍵表のチャンクを作って送る。マニフェストの組み立てと書き込み、健常性と保持区分の決定は cli。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// ChunkInfo はマニフェストの entries.chunks[]・chunk_keys.chunks[] の要素(Key はチャンク鍵の base64)。
type ChunkInfo struct {
	ID        string `json:"id"`
	VersionID string `json:"version_id"`
	Key       string `json:"key"`
}

// ItemRef は取得の結果の errors[]・warnings[] の要素(パスは item_ref に置き換えた後)。
type ItemRef struct {
	Code    string  `json:"code"`
	ItemRef *string `json:"item_ref"` // 対象全体の要素は null
}

// TargetOutcome は対象ごとの取得の結果。
type TargetOutcome struct {
	ID, Kind, Status  string
	Files, Bytes      int64
	ChangedDuringRead int64
	Dumps             []string // dump の項目(送った順)
	Errors, Warnings  []ItemRef
	ErrorsOmitted     int64
	WarningsOmitted   int64
	ErrorCode         string // status が failed のとき(それ以外は空)
}

// IngestResult は取得の入口の出力(spec 7.5 backup の流れ 1)。cli はこれからマニフェストを組む。
type IngestResult struct {
	ManifestID       string
	Targets          []TargetOutcome
	EntriesCount     int
	EntriesChunks    []ChunkInfo
	KeyTableCount    int
	KeyTableChunks   []ChunkInfo
	ChunksReferenced int
	ChunksNew        int64
	BytesNew         int64
	ChunkKeysMissing int
	// ReferencedChunkIDs はこの世代が参照するチャンクの ID(データ・エントリ・鍵表)。cli がマニフェストを書いた後に Index.Commit へ渡す。
	ReferencedChunkIDs []string
	// TargetsMismatch は取得の前の自己診断で、受け口の許可リストと定義が合わず取らなかった対象の id(cli が RECEIVER_TARGETS_MISMATCH を警告にする)。
	TargetsMismatch []string
	Duration        time.Duration
}

func newManifestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", errInternal("")
	}
	return "m-" + hex.EncodeToString(b), nil
}

// Run は contract.Acquire を満たす。結果は backup の結果の形(取得の部分。health・checks・保持区分は cli が決める)。
// すべての対象が failed なら ALL_TARGETS_FAILED。cli が対象ごとの結果を要るときは Ingest を使う。
func (a *Acquire) Run(ctx context.Context, c contract.Call) (contract.Reply, error) {
	r, err := a.Ingest(ctx, c)
	if err != nil {
		return contract.Reply{}, err
	}
	return r.reply()
}

// Ingest は取得の入口(spec 7.5「backup の流れ」の 1)。取り消された文脈は保管先と索引へ書かずに RUN_ABORTED。
//
// マニフェストを組むのは cli(spec 7.5 の acquire の「持たないもの」と backup の流れ 2〜5)で、第 1 段のハーネスと U1f-1 の cli は次の順に呼ぶ:
//  1. Acquire.Selftest(取得の前の自己診断)。続けて Ingest の結果 r を受ける。r.Targets がすべて failed なら マニフェストを書かない(Run の応答は ALL_TARGETS_FAILED)。
//  2. Retention.MissingRefs で参照の存在を照合する(欠けた参照は送り直し、送り直せなければ health を incomplete)。
//  3. 健常性を決める: r.Targets に ok でないものがある、または r.ChunkKeysMissing > 0 なら incomplete(CHUNK_KEY_MISSING を警告)。チェックは無い(イテレーション 1)。
//     保持区分は Retention.Class。
//  4. KeyService.NewSnapshotKey(r.ManifestID) でスナップショット鍵を作り、本体 JSON(sitate.manifest/v1: manifest_id=r.ManifestID、targets[] は r.Targets から
//     id・kind・status・files・bytes・changed_during_read・dumps、entries={format,count:r.EntriesCount,chunks:r.EntriesChunks}、
//     chunk_keys={format,count:r.KeyTableCount,chunks:r.KeyTableChunks}、stats={chunks_referenced:r.ChunksReferenced,chunks_new:r.ChunksNew,bytes_new:r.BytesNew}、
//     definition・def_hash・parent・retention_class・health・checks:[])を Codec.Encode(MagicManifest, ...) で符号化し、ObjectStore.Put("manifests/<id>", class) で書く。
//  5. StateFiles で snapshots[] に足し、Recorder で記録する(r.Targets[].Errors・Warnings は run_end の targets[] へ)。最後に (*Index).Commit(manifestID, r.ReferencedChunkIDs)
//     で仮の first_seen(first_seen_pending)を確定する。
//
// Run は同じ呼び出しを backup の結果の形(取得の部分)の応答にしたもので、cli が r の全体を要るときは Ingest を使う。
func (a *Acquire) Ingest(ctx context.Context, c contract.Call) (IngestResult, error) {
	if err := need(a.Store, a.State, a.Index, a.Codec, a.KeyS, a.Adapters); err != nil {
		return IngestResult{}, err
	}
	if e := ctxErr(ctx); e != nil {
		return IngestResult{}, e
	}
	ok, err := getBool(ctx, a.State, "initialized")
	if err != nil {
		return IngestResult{}, err
	}
	if !ok {
		return IngestResult{}, mkErr(contract.CodeNotInitialized, map[string]any{}, nextOf(contract.CodeNotInitialized, false, "init"))
	}
	raw, err := a.runDefinition(ctx)
	if err != nil {
		return IngestResult{}, err
	}
	return a.ingest(ctx, c, raw)
}

func (a *Acquire) ingest(ctx context.Context, c contract.Call, rawDef []byte) (IngestResult, error) {
	def, err := parseCheckDef(rawDef)
	if err != nil {
		return IngestResult{}, err
	}
	mid, err := newManifestID()
	if err != nil {
		return IngestResult{}, err
	}
	start := time.Now()
	if a.Clock != nil {
		if t, e := a.Clock.Now(ctx); e == nil {
			start = t
		}
	}
	pull, mism, err := a.preflight(ctx, def)
	if err != nil {
		return IngestResult{}, err
	}
	sink := &chunkSink{a: a, manifestID: mid}
	res := IngestResult{ManifestID: mid}
	var entries []entry
	byTarget := map[string][]entry{}
	chunks := map[string]chunkOut{}
	for _, t := range def.Targets {
		if mism[t.ID] { // 受け口の許可リストと定義が違う対象は取らず failed にする(何も取らない。spec 3.3)
			res.Targets = append(res.Targets, TargetOutcome{ID: t.ID, Kind: targetSpec(t).Kind, Status: "failed", ErrorCode: string(contract.CodeReceiverTargetsMismatch)})
			res.TargetsMismatch = append(res.TargetsMismatch, t.ID)
			continue
		}
		out, ents, chs, err := a.runTarget(ctx, c, sink, t, pull)
		if err != nil {
			return res, err
		}
		res.Targets = append(res.Targets, out)
		if out.Status != "failed" {
			byTarget[t.ID] = ents
			entries = append(entries, ents...)
			for _, ch := range chs {
				chunks[ch.ID] = ch
			}
		}
	}
	stale, err := a.finishIngest(ctx, sink, &res, entries, chunks)
	if err != nil {
		return res, err
	}
	if err := a.storeFileIndexes(res.Targets, byTarget, stale); err != nil {
		return res, err
	}
	if a.Clock != nil {
		if t, e := a.Clock.Now(ctx); e == nil {
			res.Duration = t.Sub(start)
		}
	}
	return res, nil
}

// finishIngest はエントリと鍵表のチャンクを作って送り、結果に参照を入れる。
func (a *Acquire) finishIngest(ctx context.Context, sink *chunkSink, res *IngestResult, entries []entry, chunks map[string]chunkOut) (map[string]bool, error) {
	if len(entries) == 0 {
		return nil, nil // 取れた対象が無い: マニフェストを書かない(cli が ALL_TARGETS_FAILED にする)
	}
	sortEntries(entries)
	rows, missingIDs, err := a.keyRows(ctx, sink, entries, chunks)
	if err != nil {
		return nil, err
	}
	missing := len(missingIDs)
	refreshVersions(entries, rows) // エントリの本文を作る前に、ranges の版を現在の値に揃える
	stale := a.dropStaleIndexes(entries, missingIDs)
	var eb strings.Builder
	for _, e := range entries {
		b, _ := json.Marshal(e)
		eb.Write(b)
		eb.WriteByte('\n')
	}
	ec, err := sink.chunkBytes(ctx, []byte(eb.String()))
	if err != nil {
		return nil, err
	}
	var kb strings.Builder
	for _, r := range rows {
		b, _ := json.Marshal(map[string]string{"id": r.ID, "v": r.VID, "k": r.keyB64()})
		kb.Write(b)
		kb.WriteByte('\n')
	}
	kc, err := sink.chunkBytes(ctx, []byte(kb.String()))
	if err != nil {
		return nil, err
	}
	res.EntriesCount, res.EntriesChunks = len(entries), toInfos(ec)
	res.KeyTableCount, res.KeyTableChunks = len(rows), toInfos(kc)
	res.ChunksReferenced, res.ChunkKeysMissing = len(rows), missing
	for _, l := range [][]chunkOut{rows, ec, kc} {
		for _, c := range l {
			res.ReferencedChunkIDs = append(res.ReferencedChunkIDs, c.ID)
		}
	}
	res.ChunksNew, res.BytesNew = sink.stats.New, sink.stats.BytesNew
	return stale, nil
}

func toInfos(l []chunkOut) []ChunkInfo {
	r := []ChunkInfo{}
	for _, c := range l {
		r = append(r, ChunkInfo{ID: c.ID, VersionID: c.VID, Key: c.keyB64()})
	}
	return r
}

// keyRows は全エントリ(前の世代のチャンクも、記録した retain-until が目標 A に足りなければ延ばしてから参照する。4.5)の ranges が指すデータのチャンクの鍵表の行(ID の順)。この実行で作った鍵に無いもの(前回から変化の無いファイルが指す前の世代のチャンク)は
// 索引から引く。鍵の無いチャンクは行にせず数える(世代を incomplete にするのは cli)。
func (a *Acquire) keyRows(ctx context.Context, sink *chunkSink, entries []entry, chunks map[string]chunkOut) ([]chunkOut, map[string]bool, error) {
	seen := map[string]chunkOut{}
	missing := map[string]bool{}
	for _, e := range entries {
		if e.Ranges == nil {
			continue
		}
		for _, r := range *e.Ranges {
			if _, ok := seen[r.ID]; ok || missing[r.ID] {
				continue
			}
			if c, ok := chunks[r.ID]; ok {
				seen[r.ID] = c
				continue
			}
			c, ok, err := a.lookupKey(ctx, sink, r)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				seen[r.ID] = c
			} else {
				missing[r.ID] = true
			}
		}
	}
	rows := make([]chunkOut, 0, len(seen))
	for _, c := range seen {
		rows = append(rows, c)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, missing, nil
}

func (a *Acquire) lookupKey(ctx context.Context, sink *chunkSink, r rng) (chunkOut, bool, error) {
	ref, err := a.Index.Lookup(ctx, r.ID)
	if err != nil {
		return chunkOut{}, false, err
	}
	if ref == nil || len(ref.WrappedKey) == 0 || ref.VersionID == "" {
		return chunkOut{}, false, nil
	}
	ok, err := sink.retainOK(ctx, r.ID, ref)
	if err != nil {
		return chunkOut{}, false, err
	}
	if !ok { // 延長できない(前回から変化の無いファイルは読み直していないため送り直せない)
		return chunkOut{}, false, nil
	}
	k, err := a.KeyS.UnwrapChunkKey(ref.WrappedKey, r.ID)
	if err != nil {
		return chunkOut{}, false, nil
	}
	return chunkOut{ID: r.ID, VID: ref.VersionID, Key: k}, true, nil
}

// refreshVersions は全エントリの ranges の版 ID を、鍵表の行(この実行で確かめた現在の版)に揃える。前回から変化の無いファイルの写しも、
// 送り直したチャンクの新しい版を指す(古い版は延長されず、prune に消される)。spec 4.5。
func refreshVersions(entries []entry, rows []chunkOut) {
	cur := map[string]string{}
	for _, r := range rows {
		cur[r.ID] = r.VID
	}
	for _, e := range entries {
		if e.Ranges == nil {
			continue
		}
		rs := *e.Ranges
		for i := range rs {
			if v, ok := cur[rs[i].ID]; ok {
				rs[i].VID = v
			}
		}
	}
}

// dropStaleIndexes は、参照できないチャンクを指すエントリを持つ対象の index/files を捨て(次の backup が初回として全部を読み直し、送り直す)、その対象の集合を返す。
func (a *Acquire) dropStaleIndexes(entries []entry, missing map[string]bool) map[string]bool {
	stale := map[string]bool{}
	fi, ok := a.fidx()
	if !ok || len(missing) == 0 {
		return stale
	}
	for _, e := range entries {
		if e.Ranges == nil || stale[e.Target] {
			continue
		}
		for _, r := range *e.Ranges {
			if missing[r.ID] {
				_ = os.Remove(fi.path(e.Target))
				stale[e.Target] = true
				break
			}
		}
	}
	return stale
}

// storeFileIndexes は status が ok の対象の index/files/<id> を置き換える(取れなかった・一部だけの対象は置き換えない。spec 4.5)。
func (a *Acquire) storeFileIndexes(targets []TargetOutcome, byTarget map[string][]entry, stale map[string]bool) error {
	fi, ok := a.fidx()
	if !ok {
		return nil
	}
	for _, t := range targets {
		if t.Status == "ok" && !stale[t.ID] {
			if err := fi.store(t.ID, byTarget[t.ID]); err != nil {
				return err
			}
		}
	}
	return nil
}

// matchTargets は取得の前に受け口の selftest で定義の対象と許可リストを突き合わせる。受け口に届かなければ STORAGE_UNREACHABLE(取得に入らない)、
// 転送や鍵の行の項目(RECEIVER_SELFTEST_FAILED)が外れていれば取得を始めずに失敗、対象が合わなければその id を返す。pull 接続が無い(取得アダプタだけの試験の構成)ときは何もしない。
func (a *Acquire) matchTargets(ctx context.Context, pull *contract.SSHConn, def checkDef) (map[string]bool, error) {
	mism := map[string]bool{}
	if pull == nil {
		return mism, nil
	}
	doc, err := a.pullSelftest(ctx, *pull)
	if err != nil {
		return nil, err
	}
	want := ""
	if rb, _ := a.State.Get(ctx, "receiver_hash"); len(rb) > 0 {
		_ = json.Unmarshal(rb, &want)
	}
	var failed []string
	for _, it := range evalSelftest(doc, want, def) {
		switch {
		case it.OK:
		case *it.Code == string(contract.CodeReceiverSelftestFailed):
			failed = append(failed, it.Name)
		case *it.Code == string(contract.CodeReceiverTargetsMismatch) && it.TargetID != nil:
			mism[*it.TargetID] = true
		}
	}
	if len(failed) > 0 {
		return nil, mkErr(contract.CodeReceiverSelftestFailed, map[string]any{"failed": failed}, nextOf(contract.CodeReceiverSelftestFailed, false, "source", "enroll"))
	}
	return mism, nil
}

// cleanWork は異常終了の後に残った作業ディレクトリ(平文の前回の索引の写しを含む)を消す。実行は排他の下で 1 つだけなので、他の実行の物は無い。
func (a *Acquire) cleanWork() {
	if a.Dir == "" && a.WorkDir == "" {
		return
	}
	ms, _ := filepath.Glob(filepath.Join(a.workDir(), "r-*"))
	for _, m := range ms {
		_ = os.RemoveAll(m)
	}
}

// preflight は取得の前の準備: 作業ディレクトリの掃除、pull 接続、受け口の自己診断との突き合わせ。
func (a *Acquire) preflight(ctx context.Context, def checkDef) (*contract.SSHConn, map[string]bool, error) {
	a.cleanWork()
	var pull *contract.SSHConn
	if a.Container != nil || a.SSH != nil {
		var err error
		if pull, err = a.pullConnection(ctx, def.Host); err != nil {
			return nil, nil, err
		}
	}
	mism, err := a.matchTargets(ctx, pull, def)
	return pull, mism, err
}
