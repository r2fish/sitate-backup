package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// verifyRunsPerMonth は抜き取りの件数(月あたり VerifySamplePerMonth)を日次の verify に等分する日数(1 か月 = 30 日)。
const verifyRunsPerMonth = 30

type sampleCursor struct {
	WrittenAt time.Time `json:"written_at"`
	Key       string    `json:"key"`
	VersionID string    `json:"version_id"`
}

type lastVerify struct {
	ListedAt     time.Time     `json:"listed_at"`
	SampleCursor *sampleCursor `json:"sample_cursor"`
}

// Verify は contract.Retention を満たす。recorded は記録した版 ID つきのキー(大きさ・retain-until は記録の値で、ゼロなら照合しない)。
// 存在と大きさ、記録の無い版、非現行の版・削除マーカーの数(probe/ を除く)、書いた量と保管先の量の比を照合し、retain-until は
// 前回の verify 以後に書いた版を全件、それより前の版を抜き取りで版指定で読んで記録と照合する(spec 4.2)。保管先は変えない。
// 照合を最後まで終えたら state.json の last_verify(一覧を取った時点と抜き取りの続き)を更新する。
func (r *Retention) Verify(ctx context.Context, recorded []contract.VersionRef) (contract.VerifyResult, error) {
	res := contract.VerifyResult{Missing: []contract.VersionRef{}, RetentionShort: []contract.RetentionMismatch{}, RetentionLong: []contract.RetentionMismatch{}, UnexpectedVersions: []contract.VersionRef{}}
	if err := r.ready(); err != nil {
		return res, err
	}
	if err := aborted(ctx); err != nil {
		return res, err
	}
	if err := r.Store.CheckSkew(ctx); err != nil {
		return res, err
	}
	var prev lastVerify
	if err := r.readState(ctx, "last_verify", &prev); err != nil {
		return res, err
	}
	listedAt, err := r.now(ctx) // 一覧を取る時点(境目。その後に書かれた版は次の verify が全件読む)
	if err != nil {
		return res, err
	}
	vs, err := r.Store.ListVersions(ctx, "")
	if err != nil {
		return res, err
	}
	have, rec := indexVersions(vs), map[objID]contract.VersionRef{}
	for _, ref := range recorded {
		rec[objID{ref.Key, ref.VersionID}] = ref
	}
	intents, err := r.intentKeys(ctx)
	if err != nil {
		return res, err
	}
	r.checkExistence(&res, vs, have, recorded, rec, intents)
	cursor, err := r.checkRetention(ctx, &res, have, recorded, prev)
	if err != nil {
		return res, err
	}
	if err := aborted(ctx); err != nil {
		return res, err
	}
	next := lastVerify{ListedAt: listedAt, SampleCursor: cursor}
	if cursor == nil {
		next.SampleCursor = prev.SampleCursor
	}
	return res, r.writeState(ctx, map[string]any{"last_verify": next})
}

func indexVersions(vs []contract.VersionInfo) map[objID]contract.VersionInfo {
	m := map[objID]contract.VersionInfo{}
	for _, v := range vs {
		if !v.DeleteMarker {
			m[objID{v.Key, v.VersionID}] = v
		}
	}
	return m
}

// checkExistence は存在・大きさ、記録の無い版、非現行の版と削除マーカーの数、量の比を求める。
func (r *Retention) checkExistence(res *contract.VerifyResult, vs []contract.VersionInfo, have map[objID]contract.VersionInfo, recorded []contract.VersionRef, rec map[objID]contract.VersionRef, intents map[string]bool) {
	var recordedBytes, bucketBytes int64
	for _, ref := range recorded {
		v, ok := have[objID{ref.Key, ref.VersionID}]
		if !ok || (ref.Size != 0 && v.Size != ref.Size) {
			res.Missing = append(res.Missing, ref)
		}
		recordedBytes += ref.Size
	}
	recordedKeys := map[string]bool{}
	for _, ref := range recorded {
		recordedKeys[ref.Key] = true
	}
	for _, v := range vs {
		probe := hasPrefix(v.Key, "probe/")
		switch {
		case v.DeleteMarker:
			if !probe {
				res.DeleteMarkers++
			}
			continue
		case !v.IsLatest && !probe:
			res.NoncurrentVersions++
		}
		bucketBytes += v.Size
		if _, ok := rec[objID{v.Key, v.VersionID}]; !ok && !intents[v.Key] && (recordedKeys[v.Key] || probe || hasPrefix(v.Key, "manifests/")) {
			res.UnexpectedVersions = append(res.UnexpectedVersions, contract.VersionRef{Key: v.Key, VersionID: v.VersionID, Size: v.Size})
		}
	}
	res.BytesWrittenVsBucket = float64(bucketBytes) / float64(max(recordedBytes, 1))
}

type retentionRead struct {
	ref    contract.VersionRef
	info   contract.VersionInfo
	actual time.Time
	err    error
}

// checkRetention は retain-until を版指定で読んで記録と照合し、抜き取りの続きの位置を返す(抜き取らなかったときは nil)。
func (r *Retention) checkRetention(ctx context.Context, res *contract.VerifyResult, have map[objID]contract.VersionInfo, recorded []contract.VersionRef, prev lastVerify) (*sampleCursor, error) {
	var fresh, old []retentionRead
	for _, ref := range recorded {
		v, ok := have[objID{ref.Key, ref.VersionID}]
		if !ok {
			continue // 存在しない版は missing[] に載っている
		}
		item := retentionRead{ref: ref, info: v}
		if prev.ListedAt.IsZero() || !v.LastModified.Before(prev.ListedAt) {
			fresh = append(fresh, item) // 前回の verify 以後に書いた版は全件
		} else {
			old = append(old, item)
		}
	}
	sampled, cursor := pickSample(old, prev.SampleCursor, contract.VerifySamplePerMonth/verifyRunsPerMonth)
	batch := append(fresh, sampled...)
	if err := r.readRetention(ctx, batch); err != nil {
		return nil, err
	}
	for _, b := range batch {
		if b.err != nil {
			continue // 版が無い(missing[] に載っている)
		}
		d := contract.RetentionMismatch{Key: b.ref.Key, VersionID: b.ref.VersionID, Recorded: b.ref.RetainUntil, Actual: b.actual}
		switch {
		case b.ref.RetainUntil.IsZero() || b.actual.Before(b.ref.RetainUntil.Truncate(time.Second)):
			res.RetentionShort = append(res.RetentionShort, d)
		case b.actual.After(b.ref.RetainUntil.Add(contract.TimeSkewMax)):
			res.RetentionLong = append(res.RetentionLong, d)
		}
	}
	return cursor, nil
}

// pickSample は書き込み時刻の古い順に並べた old の、前回の抜き取りの続きから n 件を取る。末尾まで読んだら先頭に戻る。
func pickSample(old []retentionRead, cur *sampleCursor, n int) ([]retentionRead, *sampleCursor) {
	if len(old) == 0 || n <= 0 {
		return nil, nil
	}
	sort.Slice(old, func(i, j int) bool { return lessRead(old[i], old[j]) })
	start := 0
	if cur != nil {
		start = sort.Search(len(old), func(i int) bool { return afterCursor(old[i], *cur) })
		if start == len(old) {
			start = 0
		}
	}
	n = min(n, len(old))
	out := make([]retentionRead, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, old[(start+i)%len(old)])
	}
	last := out[len(out)-1].info
	return out, &sampleCursor{WrittenAt: last.LastModified, Key: last.Key, VersionID: last.VersionID}
}

func lessRead(a, b retentionRead) bool {
	if !a.info.LastModified.Equal(b.info.LastModified) {
		return a.info.LastModified.Before(b.info.LastModified)
	}
	if a.info.Key != b.info.Key {
		return a.info.Key < b.info.Key
	}
	return a.info.VersionID < b.info.VersionID
}

func afterCursor(a retentionRead, c sampleCursor) bool {
	return lessRead(retentionRead{info: contract.VersionInfo{LastModified: c.WrittenAt, Key: c.Key, VersionID: c.VersionID}}, a)
}

// readRetention は batch の retain-until を StoreParallelism の並列で版指定で読む。ListVersions が運んだ値があればそれを使う(読まない)。
func (r *Retention) readRetention(ctx context.Context, batch []retentionRead) error {
	sem := make(chan struct{}, contract.StoreParallelism)
	var wg sync.WaitGroup
	for i := range batch {
		b := &batch[i]
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			b.actual, b.err = r.currentRetain(ctx, b.ref.Key, b.ref.VersionID)
		}()
	}
	wg.Wait()
	for _, b := range batch {
		if b.err != nil && !isMissing(b.err) {
			return b.err
		}
	}
	return aborted(ctx)
}

// intentKeys は書き込み予定のキー(核自身の中断した書き込み)。UNEXPECTED_VERSION から除く。Intents が nil なら空(除かない)。
func (r *Retention) intentKeys(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	if r.Intents == nil {
		return out, nil
	}
	keys, err := r.Intents(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		out[k] = true
	}
	return out, nil
}
