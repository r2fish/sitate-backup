// Command recvhelper は試験用の SITATE_RECV の実体(核の実行ファイルの代わり。EnvRecvPipe を見て RunRecvHelper を呼ぶ)。
package main

import (
	"os"

	"github.com/r2fish/sitate-backup/internal/acquire"
)

func main() {
	os.Exit(acquire.RunRecvHelper(os.Args[1:], os.Stdin, os.Stdout, os.Getenv(acquire.EnvRecvPipe)))
}
