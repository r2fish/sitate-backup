// Command sitate は核 CLI の入口。cli を呼ぶだけ。
package main

import (
	"os"

	"github.com/r2fish/sitate-backup/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
