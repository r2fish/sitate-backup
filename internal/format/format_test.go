package format

// U1a 自身の試験: 契約試験のベクトルが覆わない規則(ノンスの一意、鍵の取り違え、AAD の各部の検出、改ざん、境界)。

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, contract.KeySize) }

func reasonOf(t *testing.T, err error) string {
	t.Helper()
	var fe *contract.FormatError
	if !errors.As(err, &fe) {
		t.Fatalf("FormatError でない: %v", err)
	}
	return fe.Reason
}

func TestAEADNonceUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		s, err := (&Crypto{}).AEADSeal([]byte("x"), key(1), nil)
		if err != nil {
			t.Fatal(err)
		}
		n := string(s[:contract.NonceSize])
		if seen[n] {
			t.Fatalf("ノンスが重複した(%d 回目)", i)
		}
		seen[n] = true
	}
}

func TestAEADWrongKeyAndTamper(t *testing.T) {
	c := &Crypto{}
	s, _ := c.AEADSeal([]byte("secret"), key(1), []byte("aad"))
	if _, err := c.AEADOpen(s, key(2), []byte("aad")); err == nil {
		t.Error("別の鍵で開けた")
	}
	for i := range s {
		m := append([]byte{}, s...)
		m[i] ^= 0x80
		if _, err := c.AEADOpen(m, key(1), []byte("aad")); err == nil {
			t.Fatalf("%d バイト目の改ざんを検出しない", i)
		}
	}
	if _, err := c.AEADOpen(s[:10], key(1), []byte("aad")); err == nil {
		t.Error("短い入力を開けた")
	}
	if _, err := c.AEADSeal([]byte("x"), []byte("short"), nil); err == nil {
		t.Error("短い鍵で封緘できた")
	}
}

func TestHKDFPurposesDiffer(t *testing.T) {
	seen := map[string]string{}
	for _, p := range contract.KeyPurposes {
		k, err := (&Crypto{}).HKDF(key(5), []byte(contract.HKDFInfoPrefix+p), contract.KeySize)
		if err != nil || len(k) != contract.KeySize {
			t.Fatal(err)
		}
		if q, ok := seen[string(k)]; ok {
			t.Errorf("用途 %s と %s の鍵が同じ", p, q)
		}
		seen[string(k)] = p
	}
}

func TestArgon2idParamsMatter(t *testing.T) {
	c := &Crypto{}
	a, _ := c.Argon2id(key(1), key(2)[:16], 64, 1, 1)
	b, _ := c.Argon2id(key(1), key(2)[:16], 64, 2, 1)
	d, _ := c.Argon2id(key(1), key(3)[:16], 64, 1, 1)
	if bytes.Equal(a, b) || bytes.Equal(a, d) || len(a) != contract.KeySize {
		t.Error("パラメータやソルトが出力に効いていない")
	}
	if _, err := c.Argon2id(key(1), key(2)[:16], 64, 0, 1); err == nil {
		t.Error("反復 0 を受けた")
	}
}

func TestChunkSealWrongKeyIDAndTamper(t *testing.T) {
	c := &Codec{}
	id := strings.Repeat("ab", 32)
	plain := bytes.Repeat([]byte("data"), 1000)
	s, err := c.Seal(plain, key(1), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Open(s, key(2), id); reasonOf(t, err) != "tag" {
		t.Errorf("別のチャンク鍵: %v", err)
	}
	for i := 4; i < len(s); i++ { // 版・圧縮の印・ノンス・暗号文・タグのどれを変えても開けない
		m := append([]byte{}, s...)
		m[i] ^= 1
		if _, err := c.Open(m, key(1), id); err == nil {
			t.Fatalf("%d バイト目の改ざんを検出しない", i)
		}
	}
	if _, err := c.Seal(make([]byte, contract.ChunkMax+1), key(1), id); err == nil {
		t.Error("最大を超えるチャンクを封緘できた")
	}
	if _, err := c.Open(s[:3], key(1), id); reasonOf(t, err) != "header" {
		t.Error("短い入力は header")
	}
}

func TestChunkSealNoncesDiffer(t *testing.T) {
	id := strings.Repeat("00", 32)
	a, _ := (&Codec{}).Seal([]byte("same"), key(1), id)
	b, _ := (&Codec{}).Seal([]byte("same"), key(1), id)
	if bytes.Equal(a, b) {
		t.Error("同じ平文の封緘が同じ")
	}
}

func TestAADEachPartDetected(t *testing.T) {
	c := &Codec{}
	id := strings.Repeat("11", 16)
	h := contract.Header{Kind: contract.MagicConfig, FormatVersion: 1, Mark: []byte{0, 0, 0, 1}}
	cid := strings.Repeat("22", 32)
	obj, err := c.Encode(contract.MagicConfig, cid, h, []byte(`{}`), key(7))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Decode(contract.MagicConfig, cid, obj, key(7)); err != nil {
		t.Fatal(err)
	}
	// 印(鍵 ID)の差し替え、種類の取り違え、ID の取り違え、鍵の取り違え。
	m := append([]byte{}, obj...)
	m[8] ^= 1
	if _, err := c.Decode(contract.MagicConfig, cid, m, key(7)); reasonOf(t, err) != "tag" {
		t.Errorf("印の差し替え: %v", err)
	}
	if _, err := c.Decode(contract.MagicJournal, cid, obj, key(7)); reasonOf(t, err) != "header" {
		t.Errorf("種類の取り違え: %v", err)
	}
	if _, err := c.Decode(contract.MagicConfig, strings.Repeat("23", 32), obj, key(7)); reasonOf(t, err) != "tag" {
		t.Errorf("ID の取り違え: %v", err)
	}
	if _, err := c.Decode(contract.MagicConfig, cid, obj, key(8)); reasonOf(t, err) != "tag" {
		t.Errorf("鍵の取り違え: %v", err)
	}
	// AAD の版: 同じ見出し・ID でも版が違えば AAD が違う。
	a1, _ := c.AAD(contract.MagicConfig, cid, h)
	h.FormatVersion = 2
	a2, _ := c.AAD(contract.MagicConfig, cid, h)
	if bytes.Equal(a1, a2) {
		t.Error("AAD に形式の版が入っていない")
	}
	if _, err := c.AAD(contract.MagicConfig, id, h); err == nil {
		t.Error("長さの違う ID を受けた")
	}
	if _, err := c.Encode(contract.MagicConfig, cid, h, nil, key(7)); reasonOf(t, err) != "too_new" {
		t.Errorf("読めない版で書けた: %v", err)
	}
}

func TestKeyringStateRoundTrip(t *testing.T) {
	c := &Codec{}
	id := strings.Repeat("0f", 16)
	mark := append(append(append([]byte{0, 1, 0, 0}, 0, 0, 0, 3), 4), bytes.Repeat([]byte{9}, 16)...)
	h := contract.Header{Kind: contract.MagicKeyring, FormatVersion: 1, Mark: mark}
	obj, err := c.Encode(contract.MagicKeyring, id, h, []byte(`{"k":1}`), key(4))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := hex.DecodeString(id); !bytes.Equal(obj[5:21], got) {
		t.Error("keyring_id が見出しにない")
	}
	if hh, err := c.Header(obj); err != nil || !bytes.Equal(hh.Mark, mark) {
		t.Errorf("Header %+v %v", hh, err)
	}
	if p, err := c.Decode(contract.MagicKeyring, id, obj, key(4)); err != nil || string(p) != `{"k":1}` {
		t.Errorf("往復 %v", err)
	}
	obj[30] ^= 1 // Argon2id のパラメータ(印)を変えると開けない
	if _, err := c.Decode(contract.MagicKeyring, id, obj, key(4)); reasonOf(t, err) != "tag" {
		t.Error("パラメータの改ざんを検出しない")
	}
	sh := contract.Header{Kind: contract.MagicState, FormatVersion: 1, Mark: []byte{0, 0, 0, 1}}
	so, err := c.Encode(contract.MagicState, "index/chunks", sh, []byte("x"), key(4))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Decode(contract.MagicState, "index/files/a", so, key(4)); reasonOf(t, err) != "tag" {
		t.Error("状態ファイルのパスの取り違えを検出しない")
	}
}

func TestManifestWrappedBounds(t *testing.T) {
	c := &Codec{}
	h := contract.Header{Kind: contract.MagicManifest, FormatVersion: 1, Mark: []byte{0, 0, 0, 1}, Wrapped: make([]byte, 71)}
	if _, err := c.Encode(contract.MagicManifest, strings.Repeat("aa", 16), h, nil, key(1)); err == nil {
		t.Error("短い包んだ鍵を受けた")
	}
	if _, err := c.Header(append([]byte("SITM\x01"), make([]byte, 20)...)); reasonOf(t, err) != "header" {
		t.Error("短いマニフェストの見出しは header")
	}
}

func TestChunkBoundsAndCancel(t *testing.T) {
	data := chunkData()
	var n int
	var short int
	err := (&Codec{}).Chunk(context.Background(), bytes.NewReader(data), func(c contract.Chunk) error {
		n += c.Length
		if c.Length < contract.ChunkMin {
			short++
		}
		return nil
	})
	if err != nil || n != len(data) || short > 1 {
		t.Errorf("err=%v 合計 %d/%d 最小未満 %d 個", err, n, len(data), short)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (&Codec{}).Chunk(ctx, bytes.NewReader(data), func(contract.Chunk) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("取り消しで止まらない: %v", err)
	}
	// 最小より短い入力は 1 チャンク。
	small := make([]byte, 1000)
	_, _ = rand.Read(small)
	var got [][]byte
	_ = (&Codec{}).Chunk(context.Background(), bytes.NewReader(small), func(c contract.Chunk) error { got = append(got, c.Data); return nil })
	if len(got) != 1 || !bytes.Equal(got[0], small) {
		t.Errorf("短い入力: %d 個", len(got))
	}
}

// pinInput は固定の種から作る決定的な入力(切り方の固定用)。
func pinInput() []byte {
	data := make([]byte, 40<<20)
	x := uint64(0x9E3779B97F4A7C15)
	for i := range data {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		data[i] = byte(x >> 24)
	}
	return data
}

// pinnedCuts は pinInput の切れ目の長さの列(形式の版 1: fastcdc-v1.0.0、ChunkMin/Avg/Max)。
var pinnedCuts = []int{4498633, 5158903, 5443255, 4493147, 5244849, 3377165, 1417286, 2915437, 8574464, 819901}

func TestChunkCutsPinned(t *testing.T) {
	var got []int
	err := (&Codec{}).Chunk(context.Background(), bytes.NewReader(pinInput()), func(c contract.Chunk) error {
		got = append(got, c.Length)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range got {
		if n > contract.ChunkMax || (i < len(got)-1 && n < contract.ChunkMin) {
			t.Errorf("チャンク %d の長さ %d が範囲の外", i, n)
		}
	}
	if len(got) != len(pinnedCuts) {
		t.Fatalf("切れ目の列が固定と違う: %v", got)
	}
	for i := range got {
		if got[i] != pinnedCuts[i] {
			t.Fatalf("切れ目の列が固定と違う: %v", got)
		}
	}
}

func TestOpenRejectsOversizedExpansion(t *testing.T) {
	c := &Codec{}
	id := strings.Repeat("cd", 32)
	body := zEnc().EncodeAll(make([]byte, contract.ChunkMax+1), nil) // 展開すると ChunkMax を超える
	h := contract.Header{Kind: contract.MagicChunk, FormatVersion: 1, Mark: []byte{contract.CompressZstd}}
	aad, err := c.AAD(contract.MagicChunk, id, h)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := (&Crypto{}).AEADSeal(body, key(1), aad) // Seal を通さず正しい鍵と AAD で封緘
	if err != nil {
		t.Fatal(err)
	}
	obj := append([]byte{'S', 'I', 'T', 'C', 1, contract.CompressZstd}, sealed...)
	out, err := c.Open(obj, key(1), id)
	if err == nil {
		t.Fatalf("上限を超える展開を受けた(%d バイト)", len(out))
	}
	if reasonOf(t, err) != "tag" {
		t.Errorf("理由: %v", err)
	}
}
