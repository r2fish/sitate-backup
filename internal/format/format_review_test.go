package format

// レビュー差し戻しの試験(Chunk の読み誤り、ID の字面、見出しの検査の順、展開の上限)。

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// wrapEOFReader は少し読ませた後、io.EOF を包んだ誤り(切れたストリーム)を返す。
type wrapEOFReader struct{ sent bool }

func (r *wrapEOFReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, bytes.Repeat([]byte{1}, 100)), nil
	}
	return 0, fmt.Errorf("frame: %w", io.EOF)
}

func TestChunkWrappedEOFIsError(t *testing.T) {
	var emitted int
	err := (&Codec{}).Chunk(context.Background(), &wrapEOFReader{}, func(contract.Chunk) error { emitted++; return nil })
	if err == nil {
		t.Errorf("包んだ EOF を成功にした(emit %d 回)", emitted)
	}
}

func TestIDMustBeLowercaseHex(t *testing.T) {
	c := &Codec{}
	lower := strings.Repeat("ab", 32)
	for _, bad := range []string{strings.ToUpper(lower), "AB" + lower[2:], lower[:62] + "aB"} {
		h := contract.Header{Kind: contract.MagicChunk, FormatVersion: 1, Mark: []byte{0}}
		if _, err := c.AAD(contract.MagicChunk, bad, h); err == nil {
			t.Errorf("AAD が %q を受けた", bad)
		}
		if _, err := c.Seal([]byte("x"), key(1), bad); err == nil {
			t.Errorf("Seal が %q を受けた", bad)
		}
		s, _ := c.Seal([]byte("x"), key(1), lower)
		if _, err := c.Open(s, key(1), bad); err == nil {
			t.Errorf("Open が %q を受けた", bad)
		}
		ch := contract.Header{Kind: contract.MagicConfig, FormatVersion: 1, Mark: []byte{0, 0, 0, 1}}
		if _, err := c.Encode(contract.MagicConfig, bad, ch, []byte("{}"), key(1)); err == nil {
			t.Errorf("Encode が %q を受けた", bad)
		}
	}
}

func TestKeyringHeaderIDTamper(t *testing.T) {
	c := &Codec{}
	id := strings.Repeat("0f", 16)
	mark := append([]byte{0, 1, 0, 0, 0, 0, 0, 3, 4}, bytes.Repeat([]byte{9}, 16)...)
	h := contract.Header{Kind: contract.MagicKeyring, FormatVersion: 1, Mark: mark}
	obj, _ := c.Encode(contract.MagicKeyring, id, h, []byte("{}"), key(4))
	obj[5] ^= 1
	if _, err := c.Decode(contract.MagicKeyring, id, obj, key(4)); reasonOf(t, err) != "tag" {
		t.Errorf("見出しの keyring_id の改ざん: %v", err)
	}
}

func TestVersionCheckedBeforeHeaderLength(t *testing.T) {
	c := &Codec{}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"SITM v2 短い", func() error {
			_, e := c.Decode(contract.MagicManifest, strings.Repeat("aa", 16), append([]byte("SITM\x02"), make([]byte, 40)...), key(1))
			return e
		}()},
		{"SITC v2 短い", func() error { _, e := c.Open([]byte("SITC\x02"), key(1), strings.Repeat("aa", 32)); return e }()},
	} {
		if reasonOf(t, tc.err) != "too_new" {
			t.Errorf("%s: %v", tc.name, tc.err)
		}
	}
}

func TestDecodeVersionAndMarkReasons(t *testing.T) {
	c := &Codec{}
	id := strings.Repeat("22", 32)
	h := contract.Header{Kind: contract.MagicConfig, FormatVersion: 1, Mark: []byte{0, 0, 0, 1}}
	obj, _ := c.Encode(contract.MagicConfig, id, h, []byte("{}"), key(1))
	for v, want := range map[byte]string{2: "too_new", 0: "too_old"} {
		m := append([]byte{}, obj...)
		m[4] = v
		if _, err := c.Decode(contract.MagicConfig, id, m, key(1)); reasonOf(t, err) != want {
			t.Errorf("版 %d: %v", v, err)
		}
	}
	s, _ := c.Seal([]byte("x"), key(1), id)
	s[5] = 2 // 圧縮の印が未定義
	if _, err := c.Open(s, key(1), id); reasonOf(t, err) != "header" {
		t.Errorf("印 2: %v", err)
	}
}

func TestVerifyProofBadPublicKeyLength(t *testing.T) {
	if ok, err := (&Crypto{}).VerifyProof([]byte("r"), make([]byte, ed25519.SignatureSize), []byte{1, 2}); ok || err == nil {
		t.Errorf("短い公開鍵: %v %v", ok, err)
	}
}

// sealRaw は Seal を通さず、正しい鍵と AAD で任意の本体を封緘したチャンクを作る。
func sealRaw(t *testing.T, flag byte, body []byte, id string) []byte {
	t.Helper()
	h := contract.Header{Kind: contract.MagicChunk, FormatVersion: 1, Mark: []byte{flag}}
	aad, _ := (&Codec{}).AAD(contract.MagicChunk, id, h)
	s, err := (&Crypto{}).AEADSeal(body, key(1), aad)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte{'S', 'I', 'T', 'C', 1, flag}, s...)
}

func TestOpenRejectsOversizedPlainBody(t *testing.T) {
	id := strings.Repeat("cd", 32)
	obj := sealRaw(t, contract.CompressNone, make([]byte, contract.ChunkMax+1), id)
	if _, err := (&Codec{}).Open(obj, key(1), id); reasonOf(t, err) != "tag" {
		t.Errorf("無圧縮で上限超え: %v", err)
	}
}

func TestManifestCompressionAndLimit(t *testing.T) {
	c := &Codec{}
	id := strings.Repeat("aa", 16)
	json := bytes.Repeat([]byte(`{"a":1}`), 1000)
	h := contract.Header{Kind: contract.MagicManifest, FormatVersion: 1, Mark: []byte{0, 0, 0, 1}, Wrapped: make([]byte, 72)}
	obj, err := c.Encode(contract.MagicManifest, id, h, json, key(1))
	if err != nil {
		t.Fatal(err)
	}
	if len(obj) >= len(json) {
		t.Errorf("マニフェストを圧縮していない(%d バイト)", len(obj))
	}
	if back, err := c.Decode(contract.MagicManifest, id, obj, key(1)); err != nil || !bytes.Equal(back, json) {
		t.Errorf("往復 %v", err)
	}
	// 展開が ManifestMax を超える本体は header(spec 4.3)。
	hh, _ := c.Header(obj)
	aad, _ := c.AAD(contract.MagicManifest, id, hh)
	big := zEnc().EncodeAll(make([]byte, contract.ManifestMax+1), nil)
	s, _ := (&Crypto{}).AEADSeal(big, key(1), aad)
	bad := append(append([]byte{}, obj[:81]...), s...)
	if _, err := c.Decode(contract.MagicManifest, id, bad, key(1)); reasonOf(t, err) != "header" {
		t.Errorf("展開の上限超え: %v", err)
	}
	// 上限ちょうどは読める
	full := make([]byte, contract.ManifestMax)
	h = contract.Header{Kind: contract.MagicManifest, FormatVersion: contract.FormatVersion, Mark: hh.Mark, Wrapped: hh.Wrapped}
	enc, err := c.Encode(contract.MagicManifest, id, h, full, key(1))
	if err != nil {
		t.Fatal(err)
	}
	if back, err := c.Decode(contract.MagicManifest, id, enc, key(1)); err != nil || len(back) != contract.ManifestMax {
		t.Errorf("上限ちょうどが読めない: %d %v", len(back), err)
	}
	// 書くときに超えれば INTERNAL(component: format)
	_, err = c.Encode(contract.MagicManifest, id, h, make([]byte, contract.ManifestMax+1), key(1))
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeInternal || !strings.Contains(string(ce.Detail), `"component":"format"`) {
		t.Errorf("書くときの上限超え: %v", err)
	}
	if err := contract.Validate("cli/detail.json#/$defs/INTERNAL", ce.Detail); err != nil {
		t.Errorf("detail がスキーマに合わない: %v", err)
	}
}

// 見出しの Argon2id の値が上限を超えるとメモリを確保せずに header で拒否する(spec 4.3)。上限の値そのものは通す。
func TestArgon2idParamLimits(t *testing.T) {
	c := &Crypto{}
	salt := key(2)[:16]
	const mem, it, par = contract.Argon2idMemoryMaxKiB, contract.Argon2idIterationsMax, contract.Argon2idParallelismMax
	for _, p := range [][3]int{{mem + 1, 1, 1}, {64, it + 1, 1}, {64, 1, par + 1}, {0xFFFFFFFF, 1, 1}, {64, 0xFFFFFFFF, 1}, {64, 1, 255}} {
		if _, err := c.Argon2id(key(1), salt, p[0], p[1], p[2]); reasonOf(t, err) != "header" {
			t.Errorf("%v: %v", p, err)
		}
	}
	if _, err := c.Argon2id(key(1), salt, 64, it, par); err != nil {
		t.Errorf("上限の内側を拒否した: %v", err)
	}
}

func TestEncodeKindMismatch(t *testing.T) {
	h := contract.Header{Kind: contract.MagicJournal, FormatVersion: 1, Mark: []byte{0, 0, 0, 1}}
	if _, err := (&Codec{}).Encode(contract.MagicConfig, strings.Repeat("22", 32), h, nil, key(1)); err == nil {
		t.Error("h.Kind と kind の不一致を受けた")
	}
}

func TestArgon2idParamOverflow(t *testing.T) {
	c := &Crypto{}
	if _, err := c.Argon2id(key(1), key(2)[:16], 1<<32, 1, 1); err == nil {
		t.Error("uint32 を超えるメモリを受けた")
	}
	if _, err := c.Argon2id(key(1), key(2)[:16], 64, 1<<32, 1); err == nil {
		t.Error("uint32 を超える反復を受けた")
	}
}
