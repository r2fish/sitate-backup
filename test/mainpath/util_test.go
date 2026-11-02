//go:build mainpath

package mainpath

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
)

func sha256SumImpl(b []byte) [32]byte { return sha256.Sum256(b) }

func pemOf(blk *pem.Block) []byte { return pem.EncodeToMemory(blk) }

func hexStr(b []byte) string { return hex.EncodeToString(b) }
