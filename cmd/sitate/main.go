// Command sitate は核 CLI の入口。鍵のメモリの保護(spec 3.1.1)を始めに行い、cli を呼ぶ。
package main

import (
	"os"

	"github.com/r2fish/sitate-backup/internal/cli"
	"github.com/r2fish/sitate-backup/internal/keys"
)

func main() {
	warns := keys.HardenMemory() // gate・直接・タイマーのどの起動でも、ほかの処理より先に行う
	out := newWarnWriter(os.Stdout, warns)
	code := cli.Main(os.Args[1:], out, os.Stderr)
	out.Flush()
	os.Exit(code)
}
