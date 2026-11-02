package cli

import (
	"bufio"
	"context"
	"io"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// StateDirDefault は状態ディレクトリの置き場(spec 4.5)。
const StateDirDefault = "/var/lib/sitate"

// Deps は cli が束ねる部品と継ぎ目。注入の口はここ 1 か所(wire.go が組む。作業指示書 5.17 の完了条件 24 の結線も wire.go だけを変える)。
// nil の部品の入口は NOT_IMPLEMENTED を返す(未受け入れの部品に偽物を入れない)。試験は U0 の偽物をこの欄へ入れる。
type Deps struct {
	StateDir string
	Stdin    io.Reader

	// cli と state が実物を持つ部品(作業指示書 5.1 の表で担当が U1f-1)。
	Clock        contract.Clock
	Retention    contract.RetentionParams
	Lease        contract.Lease
	Secrets      contract.Secrets
	State        contract.StateFiles
	Recorder     contract.Recorder
	Notifier     contract.Notifier
	TestNotifier contract.TestNotifier
	Timers       contract.Timers

	// 実物を注入する継ぎ目(spec 7.5。D-0157)。SSH は acquire・restore、Bundles は restore・keys、OSProbe は keys の doctor、
	// StoreOpener は restore の OpenStore。
	SSH         contract.SSH
	Bundles     contract.BundleSource
	OSProbe     contract.OSProbe
	StoreOpener contract.StoreOpener

	// 他の WP の部品(結線は完了条件 24 と 5.18)。
	Adapters contract.AdapterRunner
	Keys     contract.Keys
	Acquire  contract.Acquire
	Restore  contract.Restore
	Approval contract.Approval
	Store    contract.Retention
	// backup の流れが使う部品(結線が入れる。spec 7.5 backup の流れ)。Ingest が nil なら backup は Acquire.Run の応答だけを使う(マニフェストを書かない)。
	Codec  contract.Codec
	Crypto contract.Crypto
	KeyS   contract.KeyService
	Index  contract.Index
	// CommitIndex は索引の仮の first_seen を確定する(acquire.Index.Commit。契約の Index に無い)。
	CommitIndex func(ctx context.Context, manifestID string, chunkIDs []string) error
	Ingest      func(ctx context.Context, c contract.Call) (IngestOut, error)
	// Object は構成した保管先(apply の時計 = StoreTime と、config/<def_hash> の書き込みに使う)。結線が入れる。nil なら apply は NOT_IMPLEMENTED。
	Object contract.ObjectStore
	// Prices は価格表(sitate.prices/v1 の JSON)を返す。結線が入れる(U5 の価格表の取り込みは完了条件 24 の外)。nil なら estimate は NOT_IMPLEMENTED。
	Prices func(ctx context.Context) ([]byte, error)
	// DefFile は定義ファイル(空なら DefFileDefault)。
	DefFile    string
	RunCleanup []contract.RunCleanup

	// Limits は実行の上限・猶予・記録の締め切り(spec 3.1.1 の規則 10・21)。試験ビルドだけが環境変数で短くする(testbuild.go)。
	Limits Limits

	// 試験が差し替える口(未設定なら標準出力と os.Exit)。監視の goroutine が封筒を書いて終了するときに使う。
	out      io.Writer
	stdinBuf *bufio.Reader
	exit     func(int)
	getenv   func(string) string
	fdOpen   func(int) io.WriteCloser
	spawn    func(ctx context.Context, argv []string) (int, error) // test-tick の子プロセスの起動口
}
