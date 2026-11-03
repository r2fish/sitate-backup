//go:build !linux

package keys

import "github.com/r2fish/sitate-backup/internal/contract"

// HardenMemory は Linux 以外では何もしない(製品の対象は Linux。開発端末のビルドを通すためだけ)。
func HardenMemory() []contract.Warning { return nil }

// Dumpable は Linux 以外では -1。
func Dumpable() int { return -1 }
