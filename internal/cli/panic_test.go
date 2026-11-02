package cli

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 部品の panic は INTERNAL にし、秘密(panic の値)を封筒にも記録にも出さず、記録 → 印の消去の順を保つ。
func TestRunFlowPanicBecomesInternal(t *testing.T) {
	f := newFlow(t)
	o := f.run(t, []string{"backup"}, func(*Deps, context.Context, contract.Call) (contract.Reply, error) {
		panic(contract.Error{Code: contract.CodeInternal, Detail: []byte(`"SECRET-VALUE"`)})
	})
	if o.err == nil || o.err.Code != contract.CodeInternal || markerExists(f.d) {
		t.Fatalf("err=%v marker=%v", o.err, markerExists(f.d))
	}
	e := f.ends(t, "run_end")
	if len(e) != 1 || !strings.Contains(string(e[0].Body), "INTERNAL") || strings.Contains(string(e[0].Body), "SECRET-VALUE") {
		t.Fatalf("run_end: %+v", e)
	}
	b, _ := o.render()
	if strings.Contains(string(b), "SECRET-VALUE") {
		t.Error("封筒に panic の値が出た")
	}
	// ロックは解放され、次の実行が取れる。
	if o2 := f.run(t, []string{"backup"}, okEntry); o2.err != nil {
		t.Errorf("panic の後の実行: %v", o2.err)
	}
}

func TestSafeEntryNoLock(t *testing.T) {
	_, err := safeEntry(func(*Deps, context.Context, contract.Call) (contract.Reply, error) { panic("x") }, &Deps{}, context.Background(), contract.Call{})
	if e := toError(err); e.Code != contract.CodeInternal {
		t.Errorf("%v", err)
	}
}

// --snapshot-key-stdin: 1 行目の後も標準入力を読み続け、終わりで ctx を取り消す(原因 errStdinClosed)。
func TestSnapshotKeyStdinEOFCancels(t *testing.T) {
	pr, pw := io.Pipe()
	d := &Deps{Stdin: pr}
	done := make(chan error, 1)
	call := make(chan struct{})
	go func() {
		_, _ = pw.Write([]byte("{\"k\":1}\n"))
		<-call
		pw.Close()
	}()
	p, _, perr := parseArgs([]string{"restore", "--snapshot-key-stdin", "--to", "x", "--endpoint", "e", "--bucket", "b"})
	if perr != nil {
		t.Skipf("引数の形: %v", perr)
	}
	c, cerr := buildCall(context.Background(), d, p, nil)
	if cerr != nil || c.SnapshotKeyIn == nil {
		t.Fatalf("%v", cerr)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	go d.watchStdin(cancel)
	select {
	case <-ctx.Done():
		t.Fatal("終わる前に取り消された")
	case <-time.After(50 * time.Millisecond):
	}
	close(call)
	select {
	case <-ctx.Done():
		done <- context.Cause(ctx)
	case <-time.After(2 * time.Second):
		done <- nil
	}
	if err := <-done; err != errStdinClosed {
		t.Errorf("cause=%v", err)
	}
	if abortReasonOf(errStdinClosed) != "signal" {
		t.Error("reason")
	}
}

func abortReasonOf(cause error) string {
	ctx, c := context.WithCancelCause(context.Background())
	c(cause)
	return abortReason(ctx)
}

func TestRunContextFlags(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	rc := runContext(ctx, "r1")
	if !rc.ProcessGroup || !rc.PdeathSig || rc.RunID != "r1" || rc.Deadline.IsZero() {
		t.Errorf("%+v", rc)
	}
}
