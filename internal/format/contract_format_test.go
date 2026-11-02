package format

// 契約試験(作業指示書 3.2): internal/contract/testdata/format/vectors.json の試験ベクトルを実物(Codec・Crypto)に通し、
// 期待値と照らす。骨格は NOT_IMPLEMENTED で落ち(harness.Implemented)、nil を返すだけの実装は値の比較で落ち、
// 仕様どおりの実装は panic しない(入力はすべて有効な値)。WP は骨格を実装し、この試験は変えない。
//
// 契約の決め(仕様に字面が無いものは U0 が決めた。internal/contract/interfaces_a.go の注釈):
//   - AEADSeal の出力は nonce(24) ∥ 暗号文 ∥ タグ(16)。AEADOpen は同じ形を受ける。
//   - SignProof の key は Ed25519 の 32 バイトのシード(合格記録の署名鍵の導出鍵)、pub は 32 バイトの公開鍵。
//   - Codec.AAD の kind は 4 バイトの種類(contract.MagicChunk など)、id は ID のバイト列の小文字 16 進(SITS だけパス)。
//   - Codec.Header の Mark は見出しの印(チャンクは圧縮の印 1 バイト、マニフェスト・設定・記録・状態は鍵 ID 4 バイト、
//     キーリングは Argon2id のパラメータ 25 バイト)。マニフェストの Wrapped は見出しに置く包んだスナップショット鍵。

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

type aeadVec struct {
	Name, Magic     string
	HeaderHex       string `json:"header_hex"`
	HeaderPrefixHex string `json:"header_prefix_hex"`
	WrappedFrom     string `json:"wrapped_from"`
	AadHex          string `json:"aad_hex"`
	KeyHex          string `json:"key_hex"`
	NonceHex        string `json:"nonce_hex"`
	PlaintextHex    string `json:"plaintext_hex"`
	ContentHex      string `json:"content_hex"` // manifest だけ: Encode に渡し Decode が返す JSON。plaintext_hex はそれを zstd で圧縮した AEAD の層の平文(spec 4.3)
	ObjectHex       string `json:"object_hex"`
}

type vectors struct {
	Readable []int     `json:"readable_versions"`
	AEAD     []aeadVec `json:"aead"`
	HKDF     struct {
		MasterHex string `json:"master_hex"`
		Derived   []struct {
			Purpose string
			KeyHex  string `json:"key_hex"`
		}
	}
	Argon []struct {
		EntropyHex  string `json:"entropy_hex"`
		SaltHex     string `json:"salt_hex"`
		MemKiB      int    `json:"mem_kib"`
		Iterations  int
		Parallelism int
		KEKHex      string `json:"kek_hex"`
	} `json:"argon2id"`
	Open []struct{ Name, Base, Mutate, Want string } `json:"open_failures"`
}

func load(t *testing.T) vectors {
	t.Helper()
	var v vectors
	harness.LoadVectors(t, "format/vectors.json", &v)
	return v
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// markLen は種類ごとの見出しの印の長さ(spec 4.3 の AAD の表)。
var markLen = map[string]int{"SITC": 1, "SITM": 4, "WRPS": 4, "SITG": 4, "SITJ": 4, "SITK": 25, "WRPC": 4, "SITS": 4}

// object はベクトル 1 件を実物に渡す形に分解したもの。
type object struct {
	v      aeadVec
	header []byte // 見出し(マニフェストは prefix ∥ 包んだ鍵)
	obj    []byte
	key    []byte
	aad    []byte
	id     string
	mark   []byte
	plain  []byte
}

func objects(t *testing.T, v vectors) map[string]object {
	t.Helper()
	out := map[string]object{}
	for _, a := range v.AEAD {
		o := object{v: a, obj: unhex(t, a.ObjectHex), key: unhex(t, a.KeyHex), aad: unhex(t, a.AadHex), plain: unhex(t, a.PlaintextHex)}
		if a.WrappedFrom != "" {
			o.header = append(unhex(t, a.HeaderPrefixHex), out[a.WrappedFrom].obj...)
		} else {
			o.header = unhex(t, a.HeaderHex)
		}
		ml := markLen[a.Magic]
		body := o.aad[5 : len(o.aad)-ml]
		o.mark = o.aad[len(o.aad)-ml:]
		o.id = hex.EncodeToString(body)
		if a.Magic == "SITS" {
			o.id = string(body[2:])
		}
		out[a.Name] = o
	}
	return out
}

func TestContractCryptoBLAKE3(t *testing.T) {
	c := &Crypto{}
	for _, in := range [][]byte{{}, []byte("abc"), bytes.Repeat([]byte{7}, 5000)} {
		var got []byte
		harness.Catch(t, func() { got = c.BLAKE3(in) })
		if want := strings.TrimPrefix(harness.Blake3Tag(in), "blake3:"); hex.EncodeToString(got) != want {
			t.Errorf("BLAKE3(%d バイト) = %x", len(in), got)
		}
	}
}

func TestContractCodecHash(t *testing.T) {
	for _, in := range [][]byte{{}, []byte("abc")} {
		var got string
		harness.Catch(t, func() { got = (&Codec{}).Hash(in) })
		if got != harness.Blake3Tag(in) {
			t.Errorf("Hash = %q, want %q", got, harness.Blake3Tag(in))
		}
	}
}

func TestContractCryptoHKDF(t *testing.T) {
	v := load(t)
	master := unhex(t, v.HKDF.MasterHex)
	for _, d := range v.HKDF.Derived {
		got, err := (&Crypto{}).HKDF(master, []byte(contract.HKDFInfoPrefix+d.Purpose), contract.KeySize)
		harness.Implemented(t, err)
		if err != nil || hex.EncodeToString(got) != d.KeyHex {
			t.Errorf("HKDF %s: %x %v", d.Purpose, got, err)
		}
	}
}

func TestContractCryptoArgon2id(t *testing.T) {
	for _, a := range load(t).Argon {
		got, err := (&Crypto{}).Argon2id(unhex(t, a.EntropyHex), unhex(t, a.SaltHex), a.MemKiB, a.Iterations, a.Parallelism)
		harness.Implemented(t, err)
		if err != nil || hex.EncodeToString(got) != a.KEKHex {
			t.Errorf("Argon2id: %x %v", got, err)
		}
	}
}

func TestContractCryptoAEADOpen(t *testing.T) {
	v := load(t)
	for name, o := range objects(t, v) {
		body := o.obj[len(o.header):] // nonce ∥ 暗号文 ∥ タグ
		got, err := (&Crypto{}).AEADOpen(body, o.key, o.aad)
		harness.Implemented(t, err)
		if err != nil || !bytes.Equal(got, o.plain) {
			t.Errorf("%s: AEADOpen %x %v", name, got, err)
		}
		bad := append([]byte{}, o.aad...)
		bad[0] ^= 1
		if p, err := (&Crypto{}).AEADOpen(body, o.key, bad); err == nil {
			t.Errorf("%s: AAD が違うのに開けた %x", name, p)
		}
		flipped := append([]byte{}, body...)
		flipped[len(flipped)-1] ^= 1
		if _, err := (&Crypto{}).AEADOpen(flipped, o.key, o.aad); err == nil {
			t.Errorf("%s: タグが違うのに開けた", name)
		}
	}
}

func TestContractCryptoAEADSeal(t *testing.T) {
	for name, o := range objects(t, load(t)) {
		s1, err := (&Crypto{}).AEADSeal(o.plain, o.key, o.aad)
		harness.Implemented(t, err)
		if err != nil || len(s1) != contract.NonceSize+len(o.plain)+contract.TagSize {
			t.Fatalf("%s: AEADSeal の長さ %d %v", name, len(s1), err)
		}
		if bytes.Contains(s1, o.plain) && len(o.plain) > 8 {
			t.Errorf("%s: 平文がそのまま入っている", name)
		}
		s2, _ := (&Crypto{}).AEADSeal(o.plain, o.key, o.aad)
		if bytes.Equal(s1[:contract.NonceSize], s2[:contract.NonceSize]) {
			t.Errorf("%s: ノンスが乱数でない", name)
		}
		// 参照実装(ベクトルの暗号の出力と同じ XChaCha20-Poly1305)で開ける。
		obj := append(append([]byte{}, o.header...), s1...)
		got, err := harness.FormatOpen(obj, len(o.header), o.key, o.aad)
		if err != nil || !bytes.Equal(got, o.plain) {
			t.Errorf("%s: 参照実装で開けない: %v", name, err)
		}
	}
}

func TestContractCryptoSignProof(t *testing.T) {
	seed := bytes.Repeat([]byte{9}, ed25519.SeedSize)
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	rec := []byte(`{"format":"sitate.restore-proof/v1"}`)
	sig, err := (&Crypto{}).SignProof(rec, seed)
	harness.Implemented(t, err)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, rec, sig) {
		t.Fatalf("署名が Ed25519 で検証できない: %x %v", sig, err)
	}
}

func TestContractCryptoVerifyProof(t *testing.T) {
	seed := bytes.Repeat([]byte{9}, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	rec := []byte(`{"format":"sitate.restore-proof/v1"}`)
	sig := ed25519.Sign(priv, rec)
	ok, err := (&Crypto{}).VerifyProof(rec, sig, pub)
	harness.Implemented(t, err)
	if err != nil || !ok {
		t.Fatalf("正しい署名を通さない: %v %v", ok, err)
	}
	if ok, _ := (&Crypto{}).VerifyProof(append(rec, ' '), sig, pub); ok {
		t.Error("記録が違うのに通した")
	}
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{8}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	if ok, _ := (&Crypto{}).VerifyProof(rec, sig, other); ok {
		t.Error("別の公開鍵で通した")
	}
}

func TestContractCodecAAD(t *testing.T) {
	for name, o := range objects(t, load(t)) {
		h := contract.Header{Kind: o.v.Magic, FormatVersion: contract.FormatVersion, Mark: o.mark}
		got, err := (&Codec{}).AAD(o.v.Magic, o.id, h)
		harness.Implemented(t, err)
		if err != nil || !bytes.Equal(got, o.aad) {
			t.Errorf("%s: AAD %x want %x (%v)", name, got, o.aad, err)
		}
	}
}

func TestContractCodecHeader(t *testing.T) {
	for name, o := range objects(t, load(t)) {
		if len(o.header) == 0 {
			continue // 包んだ鍵などは見出しを持たない
		}
		h, err := (&Codec{}).Header(o.obj)
		harness.Implemented(t, err)
		if err != nil || h.Kind != o.v.Magic || h.FormatVersion != int(o.obj[4]) || !bytes.Equal(h.Mark, o.mark) {
			t.Errorf("%s: Header %+v %v", name, h, err)
		}
		if o.v.Magic == "SITM" && !bytes.Equal(h.Wrapped, o.header[9:]) {
			t.Errorf("%s: 包んだスナップショット鍵 %x", name, h.Wrapped)
		}
	}
	_, err := (&Codec{}).Header([]byte("XXXX\x01\x00"))
	var fe *contract.FormatError
	if !errors.As(err, &fe) || fe.Reason != "header" {
		t.Errorf("印が違う見出しは FormatError{header}: %v", err)
	}
}

func TestContractCodecEncodeDecode(t *testing.T) {
	for name, o := range objects(t, load(t)) {
		if o.v.Magic == "SITC" || o.v.Magic == "WRPS" || o.v.Magic == "WRPC" {
			continue // チャンクは Seal / Open、包んだ鍵は KeyService
		}
		content := o.plain
		if o.v.ContentHex != "" {
			content = unhex(t, o.v.ContentHex)
		}
		got, err := (&Codec{}).Decode(o.v.Magic, o.id, o.obj, o.key)
		harness.Implemented(t, err)
		if err != nil || !bytes.Equal(got, content) {
			t.Errorf("%s: Decode %x %v", name, got, err)
		}
		h := contract.Header{Kind: o.v.Magic, FormatVersion: contract.FormatVersion, Mark: o.mark}
		if o.v.Magic == "SITM" {
			h.Wrapped = o.header[9:]
		}
		enc, err := (&Codec{}).Encode(o.v.Magic, o.id, h, content, o.key)
		if err != nil || !bytes.HasPrefix(enc, o.header) {
			t.Fatalf("%s: Encode の形 %x %v", name, enc, err)
		}
		if o.v.ContentHex != "" {
			checkManifestCompressed(t, enc[len(o.header):], o.key, o.aad, content)
		} else if len(enc) != len(o.header)+contract.NonceSize+len(o.plain)+contract.TagSize {
			t.Fatalf("%s: Encode の長さ %d", name, len(enc))
		}
		if back, err := (&Codec{}).Decode(o.v.Magic, o.id, enc, o.key); err != nil || !bytes.Equal(back, content) {
			t.Errorf("%s: 往復 %v", name, err)
		}
		if _, err := (&Codec{}).Decode(o.v.Magic, o.id+"00", enc, o.key); err == nil {
			t.Errorf("%s: ID が違うのに開けた", name)
		}
	}
}

// マニフェストの本体は AEAD の層で zstd の圧縮(spec 4.3、4.4)。圧縮を飛ばした実装を落とす。
func checkManifestCompressed(t *testing.T, sealed, key, aad, content []byte) {
	t.Helper()
	body, err := (&Crypto{}).AEADOpen(sealed, key, aad)
	if err != nil {
		t.Fatalf("manifest: AEAD の層で開けない: %v", err)
	}
	d, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if plain, err := d.DecodeAll(body, nil); err != nil || !bytes.Equal(plain, content) {
		t.Errorf("manifest: 本体が zstd で圧縮した JSON でない: %v", err)
	}
}

func TestContractCodecSealOpen(t *testing.T) {
	v := load(t)
	chunk := objects(t, v)["chunk-zstd"]
	key, id := chunk.key, chunk.id
	zeros := make([]byte, 64<<10)
	rnd := make([]byte, 4096)
	x := uint64(2463534242)
	for i := range rnd {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		rnd[i] = byte(x)
	}
	for _, tc := range []struct {
		name  string
		plain []byte
		flag  byte
	}{{"compressible", zeros, contract.CompressZstd}, {"incompressible", rnd, contract.CompressNone}} {
		sealed, err := (&Codec{}).Seal(tc.plain, key, id)
		harness.Implemented(t, err)
		if err != nil || len(sealed) < 6 || string(sealed[:4]) != contract.MagicChunk || int(sealed[4]) != contract.FormatVersion || sealed[5] != tc.flag {
			t.Fatalf("%s: 見出し %x %v", tc.name, sealed[:min(8, len(sealed))], err)
		}
		if tc.flag == contract.CompressZstd && len(sealed) >= len(tc.plain)/2 {
			t.Errorf("%s: 圧縮していない(%d バイト)", tc.name, len(sealed))
		}
		got, err := (&Codec{}).Open(sealed, key, id)
		if err != nil || !bytes.Equal(got, tc.plain) {
			t.Errorf("%s: 往復 %v", tc.name, err)
		}
		if _, err := (&Codec{}).Open(sealed, key, strings.Repeat("ab", 32)); err == nil {
			t.Errorf("%s: チャンク ID が違うのに開けた", tc.name)
		}
	}
}

func TestContractCodecOpenFailures(t *testing.T) {
	v := load(t)
	objs := objects(t, v)
	for _, f := range v.Open {
		base := objs[f.Base]
		obj := append([]byte{}, base.obj...)
		switch f.Mutate {
		case "flip_last":
			obj[len(obj)-1] ^= 1
		case "magic":
			obj[0] = 'X'
		case "version_2":
			obj[4] = 2
		case "version_0":
			obj[4] = 0
		case "aad_version":
			// ベクトルの AAD の版を変える代わりに、別のチャンク ID で開く(AAD が違う)。
		}
		id := base.id
		if f.Mutate == "aad_version" {
			id = strings.Repeat("22", 32)
		}
		_, err := (&Codec{}).Open(obj, base.key, id)
		harness.Implemented(t, err)
		var fe *contract.FormatError
		if !errors.As(err, &fe) {
			t.Errorf("%s: FormatError でない: %v", f.Name, err)
			continue
		}
		if fe.Reason != f.Want {
			t.Errorf("%s: reason %q want %q", f.Name, fe.Reason, f.Want)
		}
		if f.Want == "too_new" && (fe.Required != 2 || len(fe.Supported) == 0) {
			t.Errorf("%s: required/supported %+v", f.Name, fe)
		}
	}
}

func chunkData() []byte {
	data := make([]byte, 3*contract.ChunkMax+12345)
	x := uint64(88172645463325252)
	for i := range data {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		data[i] = byte(x)
	}
	return data
}

func chunkAll(t *testing.T, data []byte) [][]byte {
	t.Helper()
	var out [][]byte
	err := (&Codec{}).Chunk(context.Background(), bytes.NewReader(data), func(c contract.Chunk) error {
		if c.Length != len(c.Data) {
			t.Errorf("Length %d と Data %d", c.Length, len(c.Data))
		}
		out = append(out, append([]byte{}, c.Data...))
		return nil
	})
	harness.Implemented(t, err)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestContractCodecChunk(t *testing.T) {
	data := chunkData()
	a, b := chunkAll(t, data), chunkAll(t, data)
	if len(a) < 3 || !bytes.Equal(bytes.Join(a, nil), data) {
		t.Fatalf("チャンクの連結が入力と違う(%d 個)", len(a))
	}
	for i, c := range a {
		if len(c) > contract.ChunkMax || (i < len(a)-1 && len(c) < contract.ChunkMin) {
			t.Errorf("チャンク %d の長さ %d が [ChunkMin, ChunkMax] の外", i, len(c))
		}
	}
	if len(a) != len(b) {
		t.Fatal("同じ入力で切り方が変わる")
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			t.Fatalf("切り方が決定的でない(%d)", i)
		}
	}
	// 先頭に挿入しても後ろの切れ目は揃う(内容に基づく切り方)。
	tailEq := 0
	for _, c := range chunkAll(t, append([]byte("xx"), data...)) {
		for _, o := range a[1:] {
			if bytes.Equal(o, c) {
				tailEq++
			}
		}
	}
	if tailEq == 0 {
		t.Error("先頭への挿入で後ろのチャンクが 1 つも揃わない(内容定義の切り方でない)")
	}
}

func TestContractCodecChunkEdges(t *testing.T) {
	stop := errors.New("stop")
	if err := (&Codec{}).Chunk(context.Background(), bytes.NewReader(chunkData()), func(contract.Chunk) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("emit の誤りを返さない: %v", err)
	}
	n := 0
	err := (&Codec{}).Chunk(context.Background(), io.LimitReader(bytes.NewReader(nil), 0), func(contract.Chunk) error { n++; return nil })
	harness.Implemented(t, err)
	if err != nil || n != 0 {
		t.Errorf("空の入力: %d 個 %v", n, err)
	}
}
