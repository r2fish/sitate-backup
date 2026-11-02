package contract_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 試験ベクトルの 1 フレーム。h は見出しの生の JSON。本体は body(文字列)・body_b64・fill(同じ 1 バイトの繰り返し)のどれか。
type vecFrame struct {
	H    json.RawMessage `json:"h"`
	Body *string         `json:"body,omitempty"`
	Fill *struct {
		Byte  string `json:"byte"`
		Count int    `json:"count"`
	} `json:"fill,omitempty"`
}

type streamVec struct {
	Description string          `json:"description"`
	Frames      []vecFrame      `json:"frames"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       *struct {
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	} `json:"error"`
}

func (f vecFrame) body() []byte {
	switch {
	case f.Body != nil:
		return []byte(*f.Body)
	case f.Fill != nil:
		return bytes.Repeat([]byte(f.Fill.Byte), f.Fill.Count)
	}
	return nil
}

func (v streamVec) bytes(t *testing.T) []byte {
	var buf bytes.Buffer
	for _, f := range v.Frames {
		var c bytes.Buffer
		if err := json.Compact(&c, f.H); err != nil {
			t.Fatal(err)
		}
		buf.Write(c.Bytes())
		buf.WriteByte('\n')
		buf.Write(f.body())
	}
	if v.Result != nil {
		var c bytes.Buffer
		_ = json.Compact(&c, v.Result)
		buf.Write(c.Bytes())
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func TestStreamVectors(t *testing.T) {
	for _, sub := range []string{"valid", "invalid"} {
		files, _ := filepath.Glob("testdata/stream/" + sub + "/*.json")
		if len(files) == 0 {
			t.Fatalf("%s: 試験ベクトルが無い", sub)
		}
		for _, f := range files {
			b, _ := os.ReadFile(f)
			var v streamVec
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			if strings.Contains(string(b), "@auto") {
				t.Errorf("%s: 未解決の @auto", f)
			}
			br := bufio.NewReader(bytes.NewReader(v.bytes(t)))
			_, err := harness.ReadStream(br, nil)
			name := sub + "/" + strings.TrimSuffix(filepath.Base(f), ".json")
			if (v.Error == nil) != (sub == "valid") {
				t.Errorf("%s: error 欄と置き場が合わない", name)
			}
			if v.Error == nil {
				if err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
				line, _ := br.ReadBytes('\n')
				if contract.Validate("adapter/acquire.json#/$defs/response", line) != nil {
					t.Errorf("%s: 最終行の結果がスキーマに合わない", name)
				}
				continue
			}
			se, ok := err.(*contract.StreamError)
			if !ok || se.Kind != v.Error.Kind || se.Reason != v.Error.Reason {
				t.Errorf("%s: 期待 %+v 実際 %v", name, *v.Error, err)
			}
		}
	}
}

// 断片の最大長はスキーマと Go の定数で一致する。
func TestStreamFragmentMaxMatchesSchema(t *testing.T) {
	b, _ := os.ReadFile("schema/adapter/stream.json")
	var d struct {
		Defs struct {
			Data struct {
				Properties struct {
					Size struct{ Maximum, Minimum int }
				}
			}
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if d.Defs.Data.Properties.Size.Maximum != contract.StreamFragmentMax || d.Defs.Data.Properties.Size.Minimum != 1 {
		t.Errorf("schema=%+v go=%d", d.Defs.Data.Properties.Size, contract.StreamFragmentMax)
	}
	b, _ = os.ReadFile("schema/receiver/pipe.json")
	if strings.Count(string(b), "1048576") < 2 || contract.StreamFragmentMax != 1048576 {
		t.Errorf("pipe の n の最大が contract.StreamFragmentMax と違う")
	}
}

// WriteFrame の出力は ReadStream で読める(型とスキーマが揃っている)。
func TestWriteFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	i64 := func(v int64) *int64 { return &v }
	_ = harness.WriteFrame(&buf, contract.StreamHeader{Type: contract.FrameTypeDump, Format: contract.DumpFormatGlobals}, nil)
	_ = harness.WriteFrame(&buf, contract.StreamHeader{Type: contract.FrameTypeData, Size: i64(2)}, []byte("ab"))
	_ = harness.WriteFrame(&buf, contract.StreamHeader{Type: contract.FrameTypeItemEnd, Bytes: i64(2), Hash: harness.Blake3Tag([]byte("ab")), Status: contract.ItemOK, ExitStatus: json.RawMessage("0")}, nil)
	all := append([]byte(nil), buf.Bytes()...)
	_ = harness.WriteFrame(&buf, contract.StreamHeader{Type: contract.FrameTypeEnd, Bytes: i64(2), Hash: harness.Blake3Tag(all), ExitStatus: json.RawMessage("0")}, nil)
	if _, err := harness.ReadStream(bufio.NewReader(&buf), nil); err != nil {
		t.Fatal(err)
	}
}
