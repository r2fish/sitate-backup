// Command files は kind acquire の同梱アダプタの骨格。実装は WP が書く。
// 契約の正本は外形(標準入出力の JSON・終了コード・adapter.json)。骨格は終了 1 と error.code: NOT_IMPLEMENTED を返す。
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stdout, `{"contract":"sitate.acquire/v1","error":{"code":"NOT_IMPLEMENTED","detail":{"feature":"acquire/files"}}}`)
	os.Exit(1)
}
