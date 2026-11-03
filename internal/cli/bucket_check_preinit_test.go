package cli

// bucket check は init の前(手順 5。候補の key_ref)でも動き、NOT_INITIALIZED を返さない(spec 3.8、3.1.2。D-0172)。
// init の後に前提とするサブコマンドは NOT_INITIALIZED のまま。

import (
	"bytes"
	"encoding/json"
	"testing"
)

func errCodeOf(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	Run(DefaultDeps(t.TempDir()), args, &out, &out)
	var env struct {
		Error *struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("封筒が読めない: %v %q", err, out.String())
	}
	if env.Error == nil {
		return ""
	}
	return env.Error.Code
}

func TestBucketCheckBeforeInitIsNotNotInitialized(t *testing.T) {
	// init の前に候補の key_ref も無ければ DEF_INVALID(spec 3.1.2 の bucket check。D-0160)。NOT_INITIALIZED ではない。
	if c := errCodeOf(t, "bucket", "check"); c != "DEF_INVALID" {
		t.Errorf("bucket check は init の前も前提で止めない(候補が無ければ DEF_INVALID): %s", c)
	}
	if c := errCodeOf(t, "verify"); c != "NOT_INITIALIZED" {
		t.Errorf("verify は init の前は NOT_INITIALIZED のまま: %q", c)
	}
}
