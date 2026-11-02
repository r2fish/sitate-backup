package cli

// 容器の中の SITATE_RECV(核の実行ファイル自身)の入口。取得アダプタが "$SITATE_RECV" <操作> <target-id> を呼ぶと、
// この実行ファイルが環境変数 SITATE_RECVPIPE つきで起動され、普通のサブコマンドではなく acquire の補助コマンドとして動く(U1c の RunRecvHelper)。
// 補助コマンドの本体は acquire にあり、結線(完了条件 24)で recvHelper に acquire.RunRecvHelper を代入する。
// 結線までは補助コマンドとして動けないので、サブコマンドとして誤動作させず 126(形の合わない呼び出し)で終わる。

import (
	"io"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// envRecvPipe は acquire.EnvRecvPipe と同じ名前(結線のときに acquire の定数で置き換える)。
const envRecvPipe = "SITATE_RECVPIPE"

// recvHelper は補助コマンドの本体(args は操作と target-id)。結線までは nil。
var recvHelper func(args []string, stdin io.Reader, stdout io.Writer, pipeDir string) int

// mainEntry は Main の本体。補助コマンドの環境変数があれば補助コマンドとして動く。
func mainEntry(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	if dir := getenv(envRecvPipe); dir != "" {
		if recvHelper == nil {
			return contract.RecvExitDenied
		}
		return recvHelper(args, stdin, stdout, dir)
	}
	d := DefaultDeps(StateDirDefault)
	d.Stdin = stdin
	return Run(d, args, stdout, stderr)
}
