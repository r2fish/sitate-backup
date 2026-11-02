package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// 100 万件以上の索引(spec 3.2.2。D-0110): 受け口の list と previous_index を根 "." を先頭に path のバイト順で 1 行ずつ突き合わせ、
// メモリがファイル数に比例しない。最大常駐サイズが CONTAINER_MEMORY_MAX(1 GiB)を十分に下回り、件数を 10 倍にしても増えないことを確かめる。
// 根直下に `!` で始まる名前を置く(D-0118 の 9)。

const bigN = 1_000_000

// bigWorld は n 件のファイルを持つ偽の受け口と前回の索引を作る(全件が前回と同じ。changedAt のファイルだけ大きさが違う)。
func bigWorld(t *testing.T, n, changedAt int) (*fakeRecv, string) {
	f := newFakeRecv(t)
	names := []string{"!a", "!b"}
	for i := 0; i < n; i++ {
		names = append(names, fmt.Sprintf("%07d", i))
	}
	var lb, pb bytes.Buffer
	lw, pw := bufio.NewWriterSize(&lb, 1<<20), bufio.NewWriterSize(&pb, 1<<20)
	_, _ = lw.Write(rootRec().encode())
	fmt.Fprintf(pw, "{\"format\":\"sitate.previdx/v1\",\"target\":\"t1\"}\n{\"path\":\".\",\"type\":\"dir\",\"size\":4096,\"mtime_ns\":1760808600000000000,\"mode\":493,\"uid\":1000,\"gid\":1000}\n")
	for i, nm := range names {
		r := fileRec(nm, int64(i%1000))
		_, _ = lw.Write(r.encode())
		pr := r
		if i == changedAt {
			pr.size++
		}
		j, _ := json.Marshal(map[string]any{"path": nm, "type": "file", "size": pr.size, "mtime_ns": pr.mtimeNS, "mode": pr.mode, "uid": pr.uid, "gid": pr.gid})
		_, _ = pw.Write(append(j, '\n'))
	}
	lw.Flush()
	pw.Flush()
	f.put("list", lb.Bytes())
	prev := filepath.Join(t.TempDir(), "t1.prev.idx")
	if err := os.WriteFile(prev, pb.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return f, prev
}

type bigRun struct {
	rss                  int64
	unchanged, files, ok int
}

// watchHWM は cmd の VmHWM を終わるまで読み続け、stop を閉じると最後の値を返す。
func watchHWM(cmd *exec.Cmd) (stop chan struct{}, hwm chan int64) {
	stop, hwm = make(chan struct{}), make(chan int64, 1)
	go func() {
		var last int64
		for {
			if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", cmd.Process.Pid)); err == nil {
				for _, l := range strings.Split(string(b), "\n") {
					var kb int64
					if _, e := fmt.Sscanf(l, "VmHWM: %d kB", &kb); e == nil {
						last = kb * 1024
					}
				}
			}
			select {
			case <-stop:
				hwm <- last
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	return stop, hwm
}

func runBig(t *testing.T, n, changedAt int) bigRun {
	f, prev := bigWorld(t, n, changedAt)
	f.setBatch(1, map[string]string{fmt.Sprintf("%07d", changedAt-2): "changed"}, []string{fmt.Sprintf("%07d", changedAt-2)}, nil, 0)
	outPath := filepath.Join(t.TempDir(), "out")
	of, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	rb, _ := json.Marshal(request("t1", "/data", &prev))
	cmd := exec.Command(adapterExe, "run")
	cmd.Stdin = bytes.NewReader(append(rb, '\n'))
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "TMPDIR=" + t.TempDir(), contract.EnvRecv + "=" + f.path()}
	cmd.Stdout = of
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// 最大常駐サイズは /proc/<pid>/status の VmHWM を終わるまで読み続けた最後の値で取る。
	// (fork した子の ru_maxrss は exec 前の親の常駐サイズを引き継ぐことがあり、この試験の親は大きな入力を持つため使わない。)
	stop, hwm := watchHWM(cmd)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	close(stop)
	of.Close()
	var r bigRun
	r.rss = <-hwm
	in, _ := os.Open(outPath)
	defer in.Close()
	br := bufio.NewReaderSize(in, 1<<20)
	_, err = harness.ReadStream(br, func(fr contract.Frame) error {
		var h contract.StreamHeader
		_ = json.Unmarshal(fr.Meta, &h)
		switch h.Type {
		case "unchanged":
			r.unchanged++
		case "file":
			r.files++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ストリームが契約に反する: %v", err)
	}
	line, _ := br.ReadBytes('\n')
	if contract.Validate("adapter/acquire.json#/$defs/result", line) == nil {
		r.ok = 1
	}
	return r
}

func TestFilesBigIndex(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("最大常駐サイズは Linux の値で確かめる")
	}
	small := runBig(t, bigN/10, 5000)
	big := runBig(t, bigN, 5000)
	t.Logf("最大常駐サイズ: 10 万件 %d バイト・100 万件 %d バイト", small.rss, big.rss)
	if big.unchanged != bigN+2-1 || big.files != 1 || big.ok != 1 {
		t.Errorf("100 万件: unchanged=%d file=%d ok=%d", big.unchanged, big.files, big.ok)
	}
	if limit := int64(contract.ContainerMemoryMax) / 8; big.rss > limit {
		t.Errorf("最大常駐サイズ %d が CONTAINER_MEMORY_MAX の 1/8(%d)を超える", big.rss, limit)
	}
	if big.rss > small.rss+64<<20 {
		t.Errorf("件数を 10 倍にすると増える(ファイル数に比例している): %d → %d", small.rss, big.rss)
	}
}
