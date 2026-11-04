package acquire

// 対象 1 つの取得: 取得アダプタの呼び出し、ストリームの照合と保管、結果の回収(spec 3.2.2)。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/r2fish/sitate-backup/internal/contract"
)

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
	FailInfo                 // failed のとき、その原因コードの detail・next(3.1.3。INTERNAL の detail は component・reason。D-0172)
	Empty             bool   // アダプタは ok だがエントリが 0 件(核が partial にして警告 TARGET_EMPTY。D-0166)
}

// FailInfo は failed の対象の detail(他は nil)と next[](他は空)。
type FailInfo struct {
	Detail json.RawMessage
	Next   []contract.Next
}

func (a *Acquire) fidx() (fileIndex, bool) {
	if a.Dir == "" || a.Codec == nil || a.KeyS == nil {
		return fileIndex{}, false
	}
	return fileIndex{dir: a.Dir, codec: a.Codec, keys: a.KeyS}, true
}

func (a *Acquire) workDir() string {
	if a.WorkDir != "" {
		return a.WorkDir
	}
	return filepath.Join(a.Dir, "work")
}

// containerWork は取得アダプタの容器の中で作業ディレクトリが見えるパス(spec 3.2.2 の previous_index の例)。
const containerWork = "/run/sitate/work"

// prevIndex は前回の索引を読み、アダプタに渡す写しを作業ディレクトリに置く。初回(索引が無い)は nil(previous_index: null)。
func (a *Acquire) prevIndex(tid, runID string) (prev []entry, reqPath *string, cleanup func(), err error) {
	cleanup = func() {}
	fi, ok := a.fidx()
	if !ok {
		return nil, nil, cleanup, nil
	}
	prev, found, err := fi.load(tid)
	if err != nil || !found {
		return nil, nil, cleanup, err
	}
	dir := filepath.Join(a.workDir(), runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, cleanup, err
	}
	name := tid + ".prev.idx"
	if err := os.WriteFile(filepath.Join(dir, name), previousIndex(tid, prev), 0o644); err != nil {
		return nil, nil, cleanup, err
	}
	p := containerWork + "/" + name
	return prev, &p, func() { _ = os.RemoveAll(dir) }, nil
}

func targetSpec(t checkTarget) contract.AcquireTarget {
	var at contract.AcquireTarget
	at.ID, at.Kind = t.ID, "files"
	if t.Postgresql == "" {
		at.Spec.Path = t.Files
		return at
	}
	// DB の対象の要求の spec は port だけ(path を置かない。受け口は対象 id で DB を引く。spec 3.2.2。D-0172)。
	at.Kind = "postgresql"
	if t.Port != nil {
		at.Spec.Port = *t.Port
	}
	return at
}

func (a *Acquire) runContext(ctx context.Context, c contract.Call) contract.RunContext {
	rc := contract.RunContext{RunID: c.RunID, ProcessGroup: true, PdeathSig: true}
	if d, ok := ctx.Deadline(); ok {
		rc.Deadline = d
	}
	return rc
}

// runTarget は 1 つの対象を取る。対象の失敗(到達不能・不一致・契約違反)は status: failed の結果にして次へ進む。
// 実行ごと止めるもの(取り消し・保管先と索引の失敗)は誤りで返す。
func (a *Acquire) runTarget(ctx context.Context, c contract.Call, sink *chunkSink, t checkTarget, pull *contract.SSHConn) (TargetOutcome, []entry, []chunkOut, error) {
	at := targetSpec(t)
	out := TargetOutcome{ID: t.ID, Kind: at.Kind, Status: "failed"}
	prev, reqPath, cleanup, err := a.prevIndex(t.ID, c.RunID)
	defer cleanup()
	if err != nil {
		return out, nil, nil, err
	}
	req, _ := json.Marshal(contract.AcquireRequest{Contract: contract.AdapterContract["acquire"], Target: at, PreviousIndex: reqPath})
	pipe := sink.start(ctx)
	ts := newTargetStream(a, t.ID, at.Kind, prev, pipe)
	rc := a.runContext(ctx, c)
	relay, stop, err := a.containerSetup(ctx, c, t.ID, reqPath != nil, pull, &rc)
	if err != nil {
		pipe.abort()
		return out, nil, nil, err
	}
	defer stop()
	raw, runErr := a.Adapters.Run(ctx, rc, "acquire", at.Kind, req, ts.onFrame)
	if relay != nil {
		if fe := relay.fatalErr(); fe != nil { // ホスト鍵の不一致は対象の失敗にせず、実行の失敗のまま
			pipe.abort()
			return out, nil, nil, fe
		}
	}
	if e := ctxErr(ctx); e != nil {
		pipe.abort()
		return out, nil, nil, e
	}
	if runErr != nil {
		pipe.abort()
		return a.failedTarget(out, runErr, pipe)
	}
	chunks, perr := pipe.finish()
	if perr != nil {
		return out, nil, nil, perr
	}
	return a.collect(out, ts, raw, chunks)
}

// failedTarget は取得アダプタの失敗を対象の失敗にする(保管先の失敗は実行の失敗)。
func (a *Acquire) failedTarget(out TargetOutcome, err error, pipe *chunkPipe) (TargetOutcome, []entry, []chunkOut, error) {
	if pipe.err != nil && !errors.Is(pipe.err, errPipeAborted) {
		return out, nil, nil, pipe.err
	}
	var sv *streamViolation
	if errors.As(err, &sv) {
		return violationOutcome(out), nil, nil, nil
	}
	out.ErrorCode = failCode(err)
	out.Detail, out.Next = failDetail(err, out.Kind)
	return out, nil, nil, nil
}

// mismatchOutcome は受け口の許可リストと定義が違う対象の失敗(RECEIVER_TARGETS_MISMATCH。detail.target_ids は対象自身)。
func mismatchOutcome(t checkTarget) TargetOutcome {
	b, _ := json.Marshal(map[string]any{"target_ids": []string{t.ID}})
	return TargetOutcome{ID: t.ID, Kind: targetSpec(t).Kind, Status: "failed", ErrorCode: string(contract.CodeReceiverTargetsMismatch), FailInfo: FailInfo{b, []contract.Next{}}}
}

// violationOutcome は契約違反の対象の失敗: INTERNAL(component: adapter、reason: output。spec 3.1.3)。
func violationOutcome(out TargetOutcome) TargetOutcome {
	out.ErrorCode = "INTERNAL"
	b, _ := json.Marshal(map[string]any{"component": "adapter", "http_status": nil, "kind": "acquire", "name": out.Kind, "reason": "output"})
	out.Detail, out.Next = b, []contract.Next{}
	return out
}

// failDetail は取得アダプタの誤りから、対象の detail・next を決める(error_code と同じ原因コードのもの)。
// アダプタの終了 1 は error の detail をそのまま使い、核が作った誤りはその detail・next を使う。
func failDetail(err error, kind string) (json.RawMessage, []contract.Next) {
	var ce *contract.Error
	if !errors.As(err, &ce) { // 型の無い誤り: INTERNAL の component・reason は分からないので null(3.1.3)
		b, _ := json.Marshal(map[string]any{"component": nil, "http_status": nil, "kind": nil, "name": nil, "reason": nil})
		return b, []contract.Next{}
	}
	if ce.Code == contract.CodeAdapterError && ce.Adapter != nil && len(ce.Adapter.Detail) > 0 && failCode(err) == ce.Adapter.Code {
		return ce.Adapter.Detail, []contract.Next{}
	}
	if failCode(err) == string(contract.CodeAdapterError) { // 閉じた形: kind・name・adapter_code(3.1.3)
		b, _ := json.Marshal(map[string]any{"kind": "acquire", "name": kind, "adapter_code": adapterCodeOf(ce)})
		return b, []contract.Next{}
	}
	next := ce.Next
	if next == nil {
		next = []contract.Next{}
	}
	return ce.Detail, next
}

// adapterCodeOf はアダプタが返した error.code(Error.Adapter の code、無ければ detail.adapter_code)。
func adapterCodeOf(ce *contract.Error) string {
	if ce.Adapter != nil {
		return ce.Adapter.Code
	}
	var d struct {
		AdapterCode string `json:"adapter_code"`
	}
	if json.Unmarshal(ce.Detail, &d) == nil {
		return d.AdapterCode
	}
	return ""
}

// failCode は取得アダプタの誤りから対象の error_code を決める。アダプタの終了 1 は ADAPTER_ERROR で、error の全体は Error.Adapter にある
// (その code が STORAGE_UNREACHABLE / RECEIVER_TARGETS_MISMATCH / INTERNAL ならそれ。detail.adapter_code にだけ載せる実装も読む)。
func failCode(err error) string {
	var ce *contract.Error
	if !errors.As(err, &ce) {
		return string(contract.CodeInternal)
	}
	if ce.Code == contract.CodeAdapterError {
		switch code := adapterCodeOf(ce); code {
		case string(contract.CodeStorageUnreachable), string(contract.CodeReceiverTargetsMismatch), string(contract.CodeInternal):
			return code
		}
	}
	return string(ce.Code)
}

// collect は結果の JSON と受け取ったストリームから対象の結果とエントリを作る。アダプタの status は信じ切らず、受け取ったストリームで確かめる
// (item_end の error・dump の失敗があるのに ok なら partial、DB の対象で dump が全部失敗なら failed・一部なら partial、files の対象に根のエントリが無ければ契約違反)。
// index/files は、ここでは置き換えない(版 ID の確定と status: ok の確認の後に ingest が行う)。
func (a *Acquire) collect(out TargetOutcome, ts *targetStream, raw json.RawMessage, chunks []chunkOut) (TargetOutcome, []entry, []chunkOut, error) {
	var res contract.AcquireResult
	if contract.Validate("adapter/acquire.json#/$defs/result", raw) != nil || json.Unmarshal(raw, &res) != nil || !ts.sawEnd {
		return violationOutcome(out), nil, nil, nil
	}
	ts.setRanges(chunks)
	ents := ts.liveEntries()
	sortEntries(ents)
	out.Status = a.verifiedStatus(ts, ents, res.Status)
	if out.Status == "violation" {
		out.Status = "failed"
		return violationOutcome(out), nil, nil, nil
	}
	out.ChangedDuringRead = ts.changed
	out.ErrorsOmitted, out.WarningsOmitted = res.Stats.ErrorsOmitted, res.Stats.WarningsOmitted
	out.Errors, out.Warnings = a.itemRefs(ts.tid, res.Errors), a.itemRefs(ts.tid, res.Warnings)
	for _, e := range ents {
		switch e.Type {
		case "file":
			out.Files++
			out.Bytes += deref64(e.Size)
		case "dump":
			out.Bytes += deref64(e.Size)
			out.Dumps = append(out.Dumps, e.Format)
		}
	}
	if out.Status == "failed" {
		out.ErrorCode = firstCode(res.Errors)
		return out, nil, nil, nil
	}
	if out.Status == "ok" && out.Files == 0 && len(out.Dumps) == 0 {
		// エントリが 0 件の対象は、アダプタが ok でも partial にする(全消去の後の世代を latest にしない。index/files も置き換えない)。
		out.Status, out.Empty = "partial", true
	}
	return out, ents, chunks, nil
}

// verifiedStatus は受け取ったストリームでアダプタの status を確かめる(下げるだけで、上げない)。"violation" は契約違反。
func (a *Acquire) verifiedStatus(ts *targetStream, ents []entry, status string) string {
	if status == "failed" {
		return status
	}
	if ts.kind == "postgresql" {
		n := 0
		for _, e := range ents {
			if e.Type == "dump" {
				n++
			}
		}
		switch {
		case n == 0:
			return "failed"
		case n < 2:
			return "partial"
		}
	} else if !ts.hasRoot(ents) {
		return "violation"
	}
	if ts.itemErrors > 0 {
		return "partial"
	}
	return status
}

// hasRoot は files の対象に根 "." のエントリ(パスが対象 id だけ)があるか。
func (t *targetStream) hasRoot(ents []entry) bool {
	for _, e := range ents {
		if string(e.Path) == t.tid {
			return true
		}
	}
	return false
}

func firstCode(l []contract.AcquireItem) string {
	if len(l) > 0 {
		return l[0].Code
	}
	return string(contract.CodeInternal)
}

// itemRefs は errors[]・warnings[] の path_b64 を、そのパスのエントリの ref(item_ref)に置き換える(パスを核の外へ出さない。T-57)。
// 先頭から AcquireErrorsMax 件まで。
func (a *Acquire) itemRefs(tid string, l []contract.AcquireItem) []ItemRef {
	r := []ItemRef{}
	for _, it := range l {
		if len(r) >= contract.AcquireErrorsMax {
			break
		}
		var ref *string
		switch {
		case it.ItemRef != nil:
			ref = it.ItemRef
		case it.PathB64 != nil:
			// 復号できない path_b64 は黙って落とさず、item_ref を null にして残す(パスは出さない)。
			if rel, err := base64.StdEncoding.DecodeString(*it.PathB64); err == nil {
				full := tid
				if string(rel) != "." {
					full = tid + "/" + string(rel)
				}
				s := a.KeyS.EntryRef(full)
				ref = &s
			}
		}
		r = append(r, ItemRef{Code: it.Code, ItemRef: ref})
	}
	return r
}

// containerSetup は容器の起動の準備: 最小のルートとパイプの置き場を作り、核の側の中継を始め、rc.Wrap を置く。Container が nil なら何もしない。
func (a *Acquire) containerSetup(ctx context.Context, c contract.Call, tid string, havePrev bool, pull *contract.SSHConn, rc *contract.RunContext) (*recvRelay, func(), error) {
	if a.Container == nil {
		return nil, func() {}, nil
	}
	if (a.Dir == "" && a.WorkDir == "") || pull == nil || a.SSH == nil {
		return nil, nil, errInternal("")
	}
	base := filepath.Join(a.workDir(), c.RunID, tid)
	t := taskDir{Root: filepath.Join(base, "rootfs"), Pipe: filepath.Join(base, "pipe")}
	name := tid + ".prev.idx"
	if havePrev {
		t.PrevIdx = filepath.Join(a.workDir(), c.RunID, name)
	}
	relay, err := startRelay(ctx, t.Pipe, tid, a.SSH, *pull)
	if err != nil {
		return nil, nil, err
	}
	rc.Wrap = a.Container.wrapFn(c.RunID, t, name)
	return relay, func() { a.Container.reap(c.RunID); relay.stop(); _ = os.RemoveAll(base) }, nil
}

// pullConnection は取得の間の pull 接続(固定値で照らす。固定値が無ければ source_seen)。
func (a *Acquire) pullConnection(ctx context.Context, host string) (*contract.SSHConn, error) {
	if a.Secrets == nil || a.SSH == nil {
		return nil, errInternal("")
	}
	key, err := loadPullKey(ctx, a.Secrets)
	if err != nil {
		return nil, err
	}
	pin, err := a.pullPin(ctx, host, key)
	if err != nil {
		return nil, err
	}
	conn := pullConn(host, key, pin)
	return &conn, nil
}
