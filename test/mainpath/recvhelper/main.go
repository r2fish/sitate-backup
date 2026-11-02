//go:build mainpath

// Command recvhelper は第 1 段のハーネスが容器の SITATE_RECV の実体として使う実行ファイル(静的リンク)。
// 核の cmd/sitate は未統合なので、acquire.RunRecvHelper を直接呼ぶ小さな主関数を置く。
package main

import (
	"os"

	"github.com/r2fish/sitate-backup/internal/acquire"
)

func main() {
	os.Exit(acquire.RunRecvHelper(os.Args[1:], os.Stdin, os.Stdout, os.Getenv(acquire.EnvRecvPipe)))
}
