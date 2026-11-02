package acquire

// 中継の止まり方と上限(書き手・読み手が消えても stop が戻る。改行の無い入力と大きな入力でメモリを増やさない)。

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func stopsWithin(t *testing.T, r *recvRelay, d time.Duration, what string) {
	t.Helper()
	done := make(chan struct{})
	go func() { r.stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s: stop が %v で戻らない", what, d)
	}
}

func openReq(t *testing.T, dir string) *os.File {
	f, err := os.OpenFile(filepath.Join(dir, pipeReqName), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// 場面 A: 補助コマンドが入力の本体の途中で死ぬ。
func TestRecvRelayStopsWhenWriterDiesMidBody(t *testing.T) {
	r, dir, _ := relayEnv(t, nil)
	req := openReq(t, dir)
	_, _ = io.WriteString(req, `{"format":"sitate.recvpipe/v1","op":"read-batch","target":"t1"}`+"\n"+`{"in":1000}`+"\n"+"0123456789")
	req.Close()
	time.Sleep(100 * time.Millisecond)
	stopsWithin(t, r, 2*time.Second, "本体の途中で書き手が消えた")
}

// 場面 B: 受け口の出力の途中で補助コマンドが死ぬ(rep の読み手がいない)。
func TestRecvRelayStopsWhenReaderIsGone(t *testing.T) {
	r, dir, _ := relayEnv(t, func(execRec) (string, int) { return strings.Repeat("y", 2<<20), 0 })
	req := openReq(t, dir)
	_, _ = io.WriteString(req, `{"format":"sitate.recvpipe/v1","op":"list","target":"t1"}`+"\n"+`{"in":0}`+"\n")
	req.Close()
	time.Sleep(300 * time.Millisecond) // 中継は rep への書き待ちで止まる
	stopsWithin(t, r, 2*time.Second, "読み手が消えた")
}

// 改行の無い入力は、行の上限で読むのをやめる(メモリを増やさない)。
func TestRecvRelayBoundsLineLength(t *testing.T) {
	r, dir, _ := relayEnv(t, nil)
	req := openReq(t, dir)
	defer req.Close()
	chunk := bytes.Repeat([]byte("a"), 1<<16)
	go func() {
		for i := 0; i < 4; i++ { // 行の上限を超えると読まれなくなり、書き手は FIFO の容量で止まる
			if _, err := req.Write(chunk); err != nil {
				return
			}
		}
	}()
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		t.Fatal("改行の無い入力を読み続けている")
	}
}

// 入力の上限を超えたら、残りを捨てて 126 で返事する(補助コマンドを待たせない)。
func TestRecvRelayInputOverLimitIs126(t *testing.T) {
	old := maxRelayInput
	maxRelayInput = 1024
	defer func() { maxRelayInput = old }()
	_, dir, src := relayEnv(t, nil)
	var out bytes.Buffer
	if code := RunRecvHelper([]string{"read-batch", "t1"}, bytes.NewReader(bytes.Repeat([]byte("p\x00"), 2000)), &out, dir); code != contract.RecvExitDenied {
		t.Errorf("上限超えは 126: %d", code)
	}
	if len(src.Execs()) != 0 {
		t.Error("上限を超えた入力を受け口へ流した")
	}
	if code := RunRecvHelper([]string{"list", "t1"}, strings.NewReader(""), io.Discard, dir); code != 0 {
		t.Errorf("その後の呼び出しは通る: %d", code)
	}
}

func TestReadLineBounded(t *testing.T) {
	if _, err := readLine(bufio.NewReaderSize(strings.NewReader(strings.Repeat("a", 100000)), 16), 64); err == nil {
		t.Error("上限を超えた行を読めた")
	}
	if l, err := readLine(bufio.NewReaderSize(strings.NewReader("abc\nrest"), 2), 64); err != nil || string(l) != "abc\n" {
		t.Errorf("通常の行: %q %v", l, err)
	}
}
