package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// SITATE_RECVPIPE があれば補助コマンドとして動く(サブコマンドの封筒を出さない)。結線済み(acquire.RunRecvHelper)。
func TestRecvHelperEntry(t *testing.T) {
	env := func(k string) string {
		if k == envRecvPipe {
			return "/pipes"
		}
		return ""
	}
	var out bytes.Buffer
	orig := recvHelper
	if orig == nil {
		t.Fatal("recvHelper が結線されていない(wire_parts.go)")
	}
	var gotArgs []string
	var gotDir string
	recvHelper = func(a []string, _ io.Reader, w io.Writer, dir string) int {
		gotArgs, gotDir = a, dir
		_, _ = w.Write([]byte("x"))
		return 7
	}
	defer func() { recvHelper = orig }()
	if c := mainEntry([]string{"list", "t1"}, strings.NewReader(""), &out, io.Discard, env); c != 7 || out.String() != "x" || gotDir != "/pipes" || len(gotArgs) != 2 {
		t.Fatalf("結線後: %d %q %v %q", c, out.String(), gotArgs, gotDir)
	}
}
