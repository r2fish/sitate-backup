// genprices は data/prices/src/ の入力から data/prices/prices.json を作る。
// 使い方(リポジトリの根で): go run ./data/prices/gen
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/r2fish/sitate-backup/data/prices"
)

func main() {
	src, err := os.ReadFile("data/prices/src/" + prices.SourceName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b, err := prices.Build(src, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile("data/prices/prices.json", b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
