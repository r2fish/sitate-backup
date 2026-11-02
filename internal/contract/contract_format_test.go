package contract_test

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var updateFormat = flag.Bool("update-format", false, "testdata/format/vectors.json の暗号の出力を埋め直す")

const formatVectorsPath = "testdata/format/vectors.json"

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type formatAEAD struct {
	Name, Magic     string
	HeaderHex       string `json:"header_hex"`
	HeaderPrefixHex string `json:"header_prefix_hex"`
	WrappedFrom     string `json:"wrapped_from"`
	AadHex          string `json:"aad_hex"`
	KeyHex          string `json:"key_hex"`
	NonceHex        string `json:"nonce_hex"`
	PlaintextHex    string `json:"plaintext_hex"`
	ObjectHex       string `json:"object_hex"`
}

type formatVectors struct {
	Version      int          `json:"version"`
	Readable     []int        `json:"readable_versions"`
	AEAD         []formatAEAD `json:"aead"`
	MasterKeyIDs []struct {
		N         uint32
		HeaderHex string `json:"header_hex"`
		Text      string
	} `json:"master_key_ids"`
	HKDF struct {
		MasterHex string `json:"master_hex"`
		Derived   []struct {
			Purpose string
			KeyHex  string `json:"key_hex"`
		}
	}
	ChunkIDs []struct {
		IDKeyHex     string `json:"id_key_hex"`
		PlaintextHex string `json:"plaintext_hex"`
		ChunkID      string `json:"chunk_id"`
	} `json:"chunk_ids"`
	FP []struct {
		IDKeyHex string `json:"id_key_hex"`
		Kind     string
		Path     string
		FP       string
	} `json:"uncovered_fp"`
	Argon []struct {
		EntropyHex  string `json:"entropy_hex"`
		SaltHex     string `json:"salt_hex"`
		MemKiB      uint32 `json:"mem_kib"`
		Iterations  uint32
		Parallelism uint8
		KEKHex      string `json:"kek_hex"`
	} `json:"argon2id"`
	Open []struct{ Name, Base, Mutate, Want string } `json:"open_failures"`
}

// filler は更新のとき doc の導出値を書き、そうでなければ比べる。
type filler struct {
	t      *testing.T
	doc    map[string]any
	update bool
}

func (f filler) at(key string, i int, field, got, want string) {
	if f.update {
		f.doc[key].([]any)[i].(map[string]any)[field] = got
	} else if got != want {
		f.t.Errorf("%s[%d].%s: %s want %s", key, i, field, got, want)
	}
}

func TestFormatVectors(t *testing.T) {
	raw, _ := os.ReadFile(formatVectorsPath)
	var doc map[string]any
	var d formatVectors
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if d.Version != contract.FormatVersion {
		t.Errorf("形式の版: %d", d.Version)
	}
	f := filler{t, doc, *updateFormat}
	objs := testFormatAEAD(t, d, f)
	testFormatKeys(t, d, f)
	testFormatOpenFailures(t, d, objs)
	if f.update {
		b, _ := json.MarshalIndent(doc, "", " ")
		if err := os.WriteFile(formatVectorsPath, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type sealed struct{ object, header, aad, key []byte }

func testFormatAEAD(t *testing.T, d formatVectors, f filler) map[string]sealed {
	objs := map[string]sealed{}
	for i, c := range d.AEAD {
		header := unhex(t, c.HeaderHex)
		if c.WrappedFrom != "" {
			header = append(unhex(t, c.HeaderPrefixHex), objs[c.WrappedFrom].object...)
			if len(header) != 4+1+4+contract.NonceSize+contract.KeySize+contract.TagSize {
				t.Errorf("%s: manifest の見出しの長さ %d", c.Name, len(header))
			}
		}
		s := sealed{header: header, aad: unhex(t, c.AadHex), key: unhex(t, c.KeyHex)}
		var err error
		if s.object, err = harness.FormatSeal(header, s.key, unhex(t, c.NonceHex), s.aad, unhex(t, c.PlaintextHex)); err != nil {
			t.Fatal(err)
		}
		objs[c.Name] = s
		f.at("aead", i, "object_hex", hex.EncodeToString(s.object), c.ObjectHex)
		pt, err := harness.FormatOpen(s.object, len(header), s.key, s.aad)
		if err != nil || hex.EncodeToString(pt) != c.PlaintextHex {
			t.Errorf("%s: 開けない %v", c.Name, err)
		}
		if len(s.aad) < 5 || s.aad[0] != byte(d.Version) {
			t.Errorf("%s: AAD の先頭は形式の版と種類", c.Name)
		} else if len(header) > 0 && (string(s.aad[1:5]) != c.Magic || harness.FormatClassify(s.object, c.Magic, d.Readable) != "ok") {
			t.Errorf("%s: 印と AAD の種類", c.Name)
		}
	}
	return objs
}

func testFormatKeys(t *testing.T, d formatVectors, f filler) {
	for _, m := range d.MasterKeyIDs {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], m.N)
		if hex.EncodeToString(b[:]) != m.HeaderHex || contract.MasterKeyIDString(m.N) != m.Text {
			t.Errorf("鍵 ID %d", m.N)
		}
	}
	master := unhex(t, d.HKDF.MasterHex)
	for _, k := range d.HKDF.Derived {
		got, err := harness.FormatDerive(master, k.Purpose)
		if err != nil || hex.EncodeToString(got) != k.KeyHex {
			t.Errorf("HKDF %s: %x", k.Purpose, got)
		}
	}
	if len(d.HKDF.Derived) != len(contract.KeyPurposes) {
		t.Errorf("用途の数")
	}
	for i, c := range d.ChunkIDs {
		got, _ := harness.FormatChunkID(unhex(t, c.IDKeyHex), unhex(t, c.PlaintextHex))
		f.at("chunk_ids", i, "chunk_id", got, c.ChunkID)
		if len(got) != 64 {
			t.Errorf("chunk id の長さ")
		}
	}
	for i, c := range d.FP {
		got, _ := harness.FormatUncoveredFP(unhex(t, c.IDKeyHex), c.Kind, []byte(c.Path))
		f.at("uncovered_fp", i, "fp", got, c.FP)
	}
	for i, c := range d.Argon {
		got := hex.EncodeToString(harness.FormatKEK(unhex(t, c.EntropyHex), unhex(t, c.SaltHex), c.MemKiB, c.Iterations, c.Parallelism))
		f.at("argon2id", i, "kek_hex", got, c.KEKHex)
	}
}

func testFormatOpenFailures(t *testing.T, d formatVectors, objs map[string]sealed) {
	for _, o := range d.Open {
		base := objs[o.Base]
		obj, aad := append([]byte{}, base.object...), append([]byte{}, base.aad...)
		switch o.Mutate {
		case "flip_last":
			obj[len(obj)-1] ^= 1
		case "aad_version":
			aad[0] = 2
		case "magic":
			obj[0] = 'X'
		case "version_2":
			obj[4] = 2
		case "version_0":
			obj[4] = 0
		}
		got := harness.FormatClassify(obj, "SITC", d.Readable)
		if got == "ok" {
			if _, err := harness.FormatOpen(obj, len(base.header), base.key, aad); err != nil {
				got = "tag"
			}
		}
		if got != o.Want {
			t.Errorf("%s: %s want %s", o.Name, got, o.Want)
		}
	}
}
