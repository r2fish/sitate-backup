package contract_test

import (
	"archive/tar"
	"bufio"
	"bytes"
	"encoding/json"
	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadVec(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestRelayVectors(t *testing.T) {
	var d struct {
		Vectors []struct {
			Name string
			OK   bool
			Raw  string
			Keys []string
		}
	}
	loadVec(t, "testdata/relay/vectors.json", &d)
	for _, v := range d.Vectors {
		var keys []string
		err := harness.ReadRelay(bufio.NewReader(strings.NewReader(v.Raw)), func(o contract.RelayObject, body []byte) error {
			if int64(len(body)) != o.Size {
				t.Errorf("%s: 本体の長さ", v.Name)
			}
			keys = append(keys, o.Key)
			return nil
		})
		if (err == nil) != v.OK {
			t.Errorf("%s: 期待 ok=%v 実際 %v", v.Name, v.OK, err)
		}
		if v.OK && strings.Join(keys, ",") != strings.Join(v.Keys, ",") {
			t.Errorf("%s: keys %v want %v", v.Name, keys, v.Keys)
		}
	}
}

// WriteRelay の出力は ReadRelay で読める。
func TestRelayWriteRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	o := contract.RelayObject{Kind: "chunk", Key: "chunks/ab/" + "ab" + strings.Repeat("c", 62), VersionID: "v1", Size: 3}
	if err := harness.WriteRelay(&buf, []contract.RelayObject{o}, [][]byte{[]byte("abc")}); err != nil {
		t.Fatal(err)
	}
	n := 0
	if err := harness.ReadRelay(bufio.NewReader(&buf), func(contract.RelayObject, []byte) error { n++; return nil }); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestConnectErrorVectors(t *testing.T) {
	var d struct {
		Vectors []struct{ Name, Stderr, Want string }
	}
	loadVec(t, "testdata/receiver/connect-error.json", &d)
	for _, v := range d.Vectors {
		if got := harness.ClassifyConnectError(v.Stderr); got != v.Want {
			t.Errorf("%s: got %s want %s", v.Name, got, v.Want)
		}
	}
}

func TestSelftestVectors(t *testing.T) {
	files, _ := filepath.Glob("testdata/receiver/selftest-*.json")
	if len(files) == 0 {
		t.Fatal("試験ベクトルが無い")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		err := contract.Validate("receiver/selftest.json#/$defs/selftest", b)
		if want := strings.Contains(f, "selftest-valid"); (err == nil) != want {
			t.Errorf("%s: %v", f, err)
		}
	}
}

// tarThenTail は実際の tar(Go の archive/tar)を書き、record の倍数までゼロ埋めし、tail を続けて、tar の終端を読み終えた位置の読み手を返す。
func tarThenTail(t *testing.T, record int, tail string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "f", Size: 3, Mode: 0o644, Format: tar.FormatPAX})
	_, _ = tw.Write([]byte("abc"))
	_ = tw.Close()
	if record > 0 {
		buf.Write(make([]byte, (record-buf.Len()%record)%record))
	}
	buf.WriteString(tail)
	r := bytes.NewReader(buf.Bytes())
	tr := tar.NewReader(r)
	for {
		if _, err := tr.Next(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestReadBatchVectors(t *testing.T) {
	var d struct {
		Input []struct {
			Name  string
			Paths []string
			OK    bool
		}
		Tail []struct {
			Name    string
			Tail    string
			Changed []string
			Record  int
			OK      bool
		}
	}
	loadVec(t, "testdata/receiver/read-batch.json", &d)
	for _, v := range d.Input {
		var ps [][]byte
		for _, p := range v.Paths {
			ps = append(ps, []byte(p))
		}
		if err := harness.ValidateReadBatchInput(ps); (err == nil) != v.OK {
			t.Errorf("input %s: %v", v.Name, err)
		}
	}
	for _, v := range d.Tail {
		r := tarThenTail(t, v.Record, v.Tail)
		got, err := harness.ParseReadBatchTail(r)
		if (err == nil) != v.OK {
			t.Errorf("tail %s: %v", v.Name, err)
			continue
		}
		if v.OK {
			if len(got) != len(v.Changed) {
				t.Errorf("tail %s: %q want %q", v.Name, got, v.Changed)
			}
			for i := range got {
				if string(got[i]) != v.Changed[i] {
					t.Errorf("tail %s[%d]: %q", v.Name, i, got[i])
				}
			}
		}
	}
}

func TestMaterializeVectors(t *testing.T) {
	var d struct {
		Names   map[string]string
		Vectors []struct {
			Type, Path, Format string
			Want               *string
		}
	}
	loadVec(t, "testdata/receiver/materialize.json", &d)
	if d.Names["custom"] != contract.DumpFileCustom || d.Names["globals"] != contract.DumpFileGlobals {
		t.Errorf("固定名が定数と違う")
	}
	for _, v := range d.Vectors {
		got, err := harness.MaterializePath(v.Type, []byte(v.Path), v.Format)
		if v.Want == nil {
			if err == nil {
				t.Errorf("%s %q: 拒否されない(%q)", v.Type, v.Path, got)
			}
		} else if err != nil || got != *v.Want {
			t.Errorf("%s %q: got %q err %v want %q", v.Type, v.Path, got, err, *v.Want)
		}
	}
}

func TestPipeVectors(t *testing.T) {
	var d struct {
		Vectors []struct {
			Name              string
			ToCore            string `json:"to_core"`
			ToHelper          string `json:"to_helper"`
			OK                bool
			Op, Target, Stdin string
			Stdout            string
			Exit              int
		}
	}
	loadVec(t, "testdata/receiver/pipe.json", &d)
	for _, v := range d.Vectors {
		rq, in, e1 := harness.ReadPipeRequest(bufio.NewReader(strings.NewReader(v.ToCore)))
		out, exit, e2 := harness.ReadPipeReply(bufio.NewReader(strings.NewReader(v.ToHelper)))
		if v.OK {
			if e1 != nil || e2 != nil || rq.Op != v.Op || rq.Target != v.Target || string(in) != v.Stdin || string(out) != v.Stdout || exit != v.Exit {
				t.Errorf("%s: %+v %v %v %q %q %d", v.Name, rq, e1, e2, in, out, exit)
			}
			continue
		}
		if e1 == nil && e2 == nil {
			t.Errorf("%s: どちらも通ってしまう", v.Name)
		}
		if exit != v.Exit {
			t.Errorf("%s: 終了状態 %d want %d", v.Name, exit, v.Exit)
		}
	}
	for _, op := range contract.RecvOps {
		if contract.Validate("receiver/pipe.json#/$defs/request", []byte(`{"format":"sitate.recvpipe/v1","op":"`+op+`","target":"t1"}`)) != nil {
			t.Errorf("RecvOps の %s が request に通らない", op)
		}
	}
}
