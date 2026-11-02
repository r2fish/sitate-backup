package acquire

// チャンクの作成と送信(spec 4.3「索引とチャンク鍵」): 切る → チャンク ID → 索引で重複排除 → 新しいものは
// 鍵を作って包み、索引へ先行書き込み → 封緘 → 保管先へ書く → 応答の版 ID を索引へ追記。

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// errPipeAborted は取得を途中で捨てたときの印(保管の失敗と区別する)。
var errPipeAborted = errors.New("取得を途中で捨てた")

// chunkOut は作った(か、既に在った)チャンク 1 つ。Start・Len はそのストリームの中の平文の位置。
type chunkOut struct {
	ID, VID    string
	Key        []byte
	Start, Len int64
}

func (c chunkOut) keyB64() string { return base64.StdEncoding.EncodeToString(c.Key) }

// sinkStats は実行の送信量(manifest の stats と backup の uploaded_bytes)。
type sinkStats struct {
	New      int64 // この実行で送ったチャンクの数
	BytesNew int64 // 保管先に置いたバイト列の大きさの合計
}

type chunkSink struct {
	a          *Acquire
	manifestID string
	mu         sync.Mutex
	stats      sinkStats
}

func chunkKeyOf(id string) string { return "chunks/" + id[:2] + "/" + id }

// handle は切り出された 1 つのチャンクを保管する(重複排除を含む)。
func (s *chunkSink) handle(ctx context.Context, data []byte, start int64) (chunkOut, error) {
	a := s.a
	id := a.KeyS.ChunkID(data)
	out := chunkOut{ID: id, Start: start, Len: int64(len(data))}
	ref, err := a.Index.Lookup(ctx, id)
	if err != nil {
		return out, err
	}
	var key []byte
	fs, pending := s.manifestID, true // 送り直しでは、既にある行の first_seen と印を保つ(確定済みの値を仮に戻さない)
	if ref != nil {
		fs, pending = ref.FirstSeen, ref.FirstSeenPending
	}
	if ref != nil && len(ref.WrappedKey) > 0 {
		if k, e := a.KeyS.UnwrapChunkKey(ref.WrappedKey, id); e == nil {
			key = k
		}
	}
	if key != nil && ref.VersionID != "" {
		ok, err := s.retainOK(ctx, id, ref)
		if err != nil {
			return out, err
		}
		if ok {
			out.VID, out.Key = ref.VersionID, key
			return out, nil
		}
	}
	if key == nil { // 鍵の無い既知のチャンクは参照せず送り直す(新しい鍵を作る)
		if key, err = s.newChunkKey(ctx, id, int64(len(data))); err != nil {
			return out, err
		}
	}
	sealed, err := a.Codec.Seal(data, key, id)
	if err != nil {
		return out, err
	}
	if e := ctxErr(ctx); e != nil {
		return out, e
	}
	res, err := a.Store.Put(ctx, chunkKeyOf(id), bytes.NewReader(sealed), contract.RetentionDaily)
	if err != nil {
		return out, err
	}
	if err := a.Index.SetVersion(ctx, id, res.VersionID); err != nil {
		return out, err
	}
	if err := s.recordPut(ctx, id, key, res, fs, pending); err != nil {
		return out, err
	}
	s.mu.Lock()
	s.stats.New++
	s.stats.BytesNew += int64(len(sealed))
	s.mu.Unlock()
	out.VID, out.Key = res.VersionID, key
	return out, nil
}

// chunkPipe は書き込んだバイト列を Codec.Chunk で切って保管する流れ(本体をためない)。
type chunkPipe struct {
	w       *io.PipeWriter
	done    chan struct{}
	chunks  []chunkOut
	err     error
	written int64
}

func (s *chunkSink) start(ctx context.Context) *chunkPipe {
	pr, pw := io.Pipe()
	p := &chunkPipe{w: pw, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		var pos int64
		err := s.a.Codec.Chunk(ctx, pr, func(c contract.Chunk) error {
			o, err := s.handle(ctx, c.Data, pos)
			if err != nil {
				return err
			}
			pos += o.Len
			p.chunks = append(p.chunks, o)
			return nil
		})
		if err != nil {
			p.err = err
		}
		_, _ = io.Copy(io.Discard, pr) // 書き手を止めない
		pr.CloseWithError(err)
	}()
	return p
}

// Write は本体を流す。切る側が失敗していれば、その誤りを返す。
func (p *chunkPipe) Write(b []byte) error {
	n, err := p.w.Write(b)
	p.written += int64(n)
	return err
}

// finish は入力を閉じて、全チャンクの保管を待つ。
func (p *chunkPipe) finish() ([]chunkOut, error) {
	_ = p.w.Close()
	<-p.done
	return p.chunks, p.err
}

// abort は途中で捨てる(これまでに送ったチャンクは残り、ロック期限の後に prune が消す)。
func (p *chunkPipe) abort() {
	_ = p.w.CloseWithError(errPipeAborted)
	<-p.done
}

// chunkBytes は内容(エントリ・鍵表)をデータと同じ規則で切って保管する。
func (s *chunkSink) chunkBytes(ctx context.Context, content []byte) ([]chunkOut, error) {
	p := s.start(ctx)
	if err := p.Write(content); err != nil {
		p.abort()
		return nil, p.err
	}
	return p.finish()
}

// rangesFor はストリームの位置 [start, start+n) を含むチャンクの範囲の並び。
func rangesFor(chunks []chunkOut, start, n int64) []rng {
	out := []rng{}
	end := start + n
	for _, c := range chunks {
		cs, ce := c.Start, c.Start+c.Len
		if ce <= start || cs >= end {
			continue
		}
		lo, hi := max64(cs, start), min64(ce, end)
		out = append(out, rng{ID: c.ID, VID: c.VID, Off: lo - cs, Len: hi - lo})
	}
	return out
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func (a *Acquire) now(ctx context.Context) time.Time {
	if a.Clock != nil {
		if t, err := a.Clock.Now(ctx); err == nil {
			return t
		}
	}
	return time.Now()
}

// targetA は新しい世代が参照するオブジェクトの延長の目標 A(spec 4.2): 日次の世代は 今 + 日次の保持日数 + 延長周期(7 日)。
// 月次になる世代の分は、7 日ごとの extend が月次の目標まで延ばす(保持区分は取得の後に cli が決める)。
func (s *chunkSink) targetA(ctx context.Context) time.Time {
	p := s.a.Retention
	if p == nil {
		p = contract.FixedRetention{}
	}
	return s.a.now(ctx).AddDate(0, 0, p.DailyDays()+7)
}

// retainOK は重複排除で既存のチャンクを参照してよいか(spec 4.3 の索引と prune の整合): 記録した retain-until が目標 A に足りなければ、
// 版 ID を指定して延長してから参照する。延長できなければ false(参照せず送り直す)。文脈の取り消しは誤りで返す。比べる値は索引に記録した値で、保管先から読み直さない。
func (s *chunkSink) retainOK(ctx context.Context, id string, ref *contract.ChunkRef) (bool, error) {
	target := s.targetA(ctx)
	if !ref.RetainUntil.IsZero() && !ref.RetainUntil.Before(target) {
		return true, nil
	}
	if err := s.a.Store.Extend(ctx, chunkKeyOf(id), ref.VersionID, target); err != nil {
		if e := ctxErr(ctx); e != nil { // 取り消しは「延ばせない」ではない: 実行を止める(索引を捨てない)
			return false, e
		}
		return false, nil
	}
	if err := s.a.Index.SetRetain(ctx, ref.VersionID, target); err != nil { // 延長したら索引の値を更新する
		return false, err
	}
	return true, nil
}

// recordPut は書き込みの応答の大きさ・ETag・retain-until を索引に残す(Reserve は同じチャンクの記録を全項目で置き換え、retain-until は SetRetain)。
func (s *chunkSink) recordPut(ctx context.Context, id string, key []byte, res contract.PutResult, fs string, pending bool) error {
	// Size は保管先に置いたバイト列の大きさ(verify の照合値)。
	wrapped, err := s.a.KeyS.WrapChunkKey(key, id)
	if err != nil {
		return err
	}
	if err := s.a.Index.Reserve(ctx, contract.ChunkRef{ChunkID: id, VersionID: res.VersionID, WrappedKey: wrapped, FirstSeen: fs, FirstSeenPending: pending,
		FormatVersion: contract.FormatVersion, Size: res.Size, ETag: res.ETag}); err != nil {
		return err
	}
	return s.a.Index.SetRetain(ctx, res.VersionID, res.RetainUntil)
}

// newChunkKey はチャンク鍵を作り、包んだ鍵を送る前に索引へ記録する(先行書き込み)。
func (s *chunkSink) newChunkKey(ctx context.Context, id string, size int64) ([]byte, error) {
	key := make([]byte, contract.KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, errInternal("")
	}
	wrapped, err := s.a.KeyS.WrapChunkKey(key, id)
	if err != nil {
		return nil, err
	}
	err = s.a.Index.Reserve(ctx, contract.ChunkRef{ChunkID: id, WrappedKey: wrapped, FirstSeen: s.manifestID, FirstSeenPending: true,
		FormatVersion: contract.FormatVersion, Size: size})
	return key, err
}
