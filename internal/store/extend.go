package store

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Extend は contract.Retention を満たす。target は "A"(保持期限が確定している世代の参照)か "B"(保護中の世代の参照と共有物)。
// 記録の retain-until より遅い目標の版にだけ要求を送る(要求数は延長する版の数。spec 4.2 の `extend` の進め方)。記録は objects[]・snapshots[] と、
// チャンクは Index の Records(延長の後は SetRetain で更新し、保管先を読み直さない)。一部が失敗しても残りを続け、failed[] に載せる
// (min_retain_until は失敗した版を含めない)。TIME_SKEW と RUN_ABORTED では残りを止め、延ばし終えた版の記録を残して誤りで返す。
func (r *Retention) Extend(ctx context.Context, target string) (contract.ExtendResult, error) {
	var res contract.ExtendResult
	if err := r.ready(); err != nil {
		return res, err
	}
	if target != "A" && target != "B" {
		return res, newErr(contract.CodeArgInvalid, map[string]any{"flag": "--target", "reason": "format"})
	}
	if err := aborted(ctx); err != nil {
		return res, err
	}
	if err := r.Store.CheckSkew(ctx); err != nil {
		return res, err
	}
	now, err := r.now(ctx)
	if err != nil {
		return res, err
	}
	s, err := r.loadRecs(ctx)
	if err != nil {
		return res, err
	}
	ts, err := r.targets(ctx, s, now, false)
	if err != nil {
		return res, err
	}
	want := ts.a
	if target == "B" {
		want = map[objID]time.Time{}
		for id := range ts.b {
			want[id] = now.AddDate(0, 0, r.params().DailyDays())
		}
	}
	ids := make([]objID, 0, len(want))
	hasChunk := false
	for id := range want {
		ids = append(ids, id)
		hasChunk = hasChunk || strings.HasPrefix(id.Key, "chunks/")
	}
	sortIDs(ids)
	run := &extendRun{r: r, rec: s.recorded(), want: want, done: map[objID]time.Time{}}
	if hasChunk {
		if err := r.addChunkRecords(ctx, run.rec); err != nil {
			return res, err
		}
	}
	run.do(ctx, ids)
	res.Extended, res.MinRetainUntil = run.extended, run.min
	res.Failed = append(append(res.Failed, ts.fails...), run.failed...)
	serr := r.saveExtend(ctx, run, now, len(res.Failed) == 0 && run.abort == nil)
	if run.abort != nil {
		return res, run.abort
	}
	return res, serr
}

// addChunkRecords はチャンクの記録(Index.Records)を rec に足す。Index が無ければ止まる(記録が無いまま延ばさない)。
func (r *Retention) addChunkRecords(ctx context.Context, rec map[objID]time.Time) error {
	if r.Index == nil {
		return nilDep()
	}
	recs, err := r.Index.Records(ctx)
	if err != nil {
		return err
	}
	for _, x := range recs {
		rec[objID{x.Key, x.VersionID}] = x.RetainUntil
	}
	return nil
}

// saveExtend は延ばした版の記録を残す。state.lock の中で読み直し、延ばした版の retain_until だけを当てる。取り消された実行でも記録用の文脈で書く。
func (r *Retention) saveExtend(ctx context.Context, run *extendRun, now time.Time, ok bool) error {
	rc, cancel := recordCtx(ctx)
	defer cancel()
	var chunks []objID
	for id, post := range run.done {
		if post.After(run.rec[id]) && strings.HasPrefix(id.Key, "chunks/") {
			chunks = append(chunks, id)
		}
	}
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].VersionID < chunks[j].VersionID })
	for _, id := range chunks {
		if err := r.Index.SetRetain(rc, id.VersionID, run.done[id]); err != nil {
			return err
		}
	}
	extra := map[string]any{}
	if ok {
		extra["last_extend_ok_at"] = now.Format(time.RFC3339)
	}
	return r.mutate(rc, func(s *stateRecs) {
		for id, post := range run.done {
			if post.After(run.rec[id]) {
				s.setRetain(id, post)
			}
		}
	}, extra)
}

type extendRun struct {
	r        *Retention
	rec      map[objID]time.Time
	want     map[objID]time.Time
	mu       sync.Mutex
	done     map[objID]time.Time
	extended int
	failed   []contract.ExtendFailure
	min      time.Time
	abort    error
}

func (e *extendRun) note(id objID, post time.Time) {
	e.done[id] = post
	if e.min.IsZero() || post.Before(e.min) {
		e.min = post
	}
}

// stop は残りを止める誤り(TIME_SKEW・RUN_ABORTED)を覚える。
func (e *extendRun) stop(err error) {
	e.mu.Lock()
	if e.abort == nil {
		e.abort = err
	}
	e.mu.Unlock()
}

func (e *extendRun) stopped() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.abort != nil
}

// do は ids を StoreParallelism の並列で延ばす。
func (e *extendRun) do(ctx context.Context, ids []objID) {
	sem := make(chan struct{}, contract.StoreParallelism)
	var wg sync.WaitGroup
	for _, id := range ids {
		if ab := aborted(ctx); ab != nil {
			e.stop(ab)
		}
		if e.stopped() {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(id objID) {
			defer func() { <-sem; wg.Done() }()
			e.one(ctx, id)
		}(id)
	}
	wg.Wait()
	if ab := aborted(ctx); ab != nil {
		e.stop(ab)
	}
}

func (e *extendRun) one(ctx context.Context, id objID) {
	target := e.want[id]
	cur, known := e.rec[id]
	if !known {
		c, err := e.r.currentRetain(ctx, id.Key, id.VersionID) // 記録の無い版だけ現在値を読む
		if err != nil {
			e.fail(ctx, id, err)
			return
		}
		cur = c
	}
	if !target.After(cur) {
		e.mu.Lock()
		e.note(id, cur)
		e.mu.Unlock()
		return
	}
	err := e.r.Store.Extend(ctx, id.Key, id.VersionID, target)
	if err != nil {
		// 記録より長く延ばされていた版を、保管先が Compliance の保持で短縮として拒否した応答は成功とみなす(拒否に限る。時計のずれ・取り消しは成功にしない)。
		if isDenied(err) && aborted(ctx) == nil {
			if actual, rerr := e.r.currentRetain(ctx, id.Key, id.VersionID); rerr == nil && !actual.Before(target) {
				e.mu.Lock()
				e.note(id, target)
				e.mu.Unlock()
				return
			}
		}
		e.fail(ctx, id, err)
		return
	}
	e.mu.Lock()
	e.extended++
	e.note(id, target)
	e.mu.Unlock()
}

func (e *extendRun) fail(ctx context.Context, id objID, err error) {
	if aborted(ctx) != nil {
		return
	}
	if code := codeOf(err); code == contract.CodeTimeSkew || code == contract.CodeRunAborted {
		e.stop(err)
		return
	}
	e.mu.Lock()
	e.failed = append(e.failed, contract.ExtendFailure{Key: id.Key, VersionID: id.VersionID, Code: codeOf(err)})
	e.mu.Unlock()
}
