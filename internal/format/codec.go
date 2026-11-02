package format

// チャンク化・圧縮・チャンクの封緘(spec 4.3)。

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/PlakarKorp/go-cdc-chunkers"
	_ "github.com/PlakarKorp/go-cdc-chunkers/chunkers/fastcdc" // 登録名 fastcdc-v1.0.0 を使う
	"github.com/klauspost/compress/zstd"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// chunkAlgorithm はチャンク化の登録名。形式の版 1 に固定する(spec 4.3。ライブラリは go.mod の版)。
const chunkAlgorithm = "fastcdc-v1.0.0"

// Codec は contract.Codec の実物。状態を持たない。
type Codec struct{}

var _ contract.Codec = (*Codec)(nil)

var (
	zEnc = sync.OnceValue(func() *zstd.Encoder {
		e, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(contract.ZstdLevel)), zstd.WithEncoderConcurrency(1))
		if err != nil {
			panic(err)
		}
		return e
	})
	zManifestDec = sync.OnceValue(func() *zstd.Decoder {
		d, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(contract.ManifestMax), zstd.WithDecoderConcurrency(1))
		if err != nil {
			panic(err)
		}
		return d
	})
	zDec = sync.OnceValue(func() *zstd.Decoder {
		d, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(contract.ChunkMax), zstd.WithDecoderConcurrency(1))
		if err != nil {
			panic(err)
		}
		return d
	})
)

// Chunk は r を FastCDC(最小 / 平均 / 最大 = ChunkMin / ChunkAvg / ChunkMax)で切り、チャンクごとに emit を呼ぶ。
// emit に渡す Data は呼び出しごとの複製で、保持してよい。emit の誤りと ctx の取り消しで止まる。
func (*Codec) Chunk(ctx context.Context, r io.Reader, emit func(contract.Chunk) error) error {
	ch, err := chunkers.NewChunker(chunkAlgorithm, r, &chunkers.ChunkerOpts{
		MinSize: contract.ChunkMin, NormalSize: contract.ChunkAvg, MaxSize: contract.ChunkMax,
	})
	if err != nil {
		return fmt.Errorf("format: チャンク化: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := ch.Next()
		if err != nil && err != io.EOF { // 包んだ EOF は切れたストリームなので誤り
			return err
		}
		if len(data) > 0 {
			if e := emit(contract.Chunk{Data: append([]byte(nil), data...), Length: len(data)}); e != nil {
				return e
			}
		}
		if err != nil {
			return nil
		}
	}
}

// Seal はチャンクを zstd で圧縮し(小さくならなければ無圧縮)、チャンク鍵で封緘する。chunkID は小文字の 16 進 64 文字。
func (c *Codec) Seal(plain, chunkKey []byte, chunkID string) ([]byte, error) {
	if len(plain) > contract.ChunkMax {
		return nil, fmt.Errorf("format: チャンクが %d バイトを超える", contract.ChunkMax)
	}
	flag, body := byte(contract.CompressNone), plain
	if z := zEnc().EncodeAll(plain, nil); len(z) < len(plain) {
		flag, body = contract.CompressZstd, z
	}
	h := contract.Header{Kind: contract.MagicChunk, FormatVersion: contract.FormatVersion, Mark: []byte{flag}}
	aad, err := c.AAD(contract.MagicChunk, chunkID, h)
	if err != nil {
		return nil, err
	}
	sealed, err := (&Crypto{}).AEADSeal(body, chunkKey, aad)
	if err != nil {
		return nil, err
	}
	return append([]byte(contract.MagicChunk+string([]byte{contract.FormatVersion, flag})), sealed...), nil
}

// Open は Seal の出力を開いて平文を返す。理由は *contract.FormatError(header / too_new / too_old / tag)。
func (c *Codec) Open(sealed, chunkKey []byte, chunkID string) ([]byte, error) {
	if err := checkPrefix(sealed, contract.MagicChunk); err != nil {
		return nil, err
	}
	h, err := c.Header(sealed)
	if err != nil {
		return nil, headerErr()
	}
	if f := h.Mark[0]; f != contract.CompressNone && f != contract.CompressZstd {
		return nil, headerErr()
	}
	aad, err := c.AAD(contract.MagicChunk, chunkID, h)
	if err != nil {
		return nil, tagErr()
	}
	body, err := (&Crypto{}).AEADOpen(sealed[layouts[contract.MagicChunk].headerLen():], chunkKey, aad)
	if err != nil {
		return nil, tagErr()
	}
	if h.Mark[0] == contract.CompressNone {
		if len(body) > contract.ChunkMax {
			return nil, tagErr()
		}
		return body, nil
	}
	plain, err := zDec().DecodeAll(body, nil)
	if err != nil {
		return nil, tagErr()
	}
	return plain, nil
}
