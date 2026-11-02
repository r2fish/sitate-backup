package contract

// 受け口(spec 3.3)と容器の補助コマンド(spec 3.2.2)の形式。

// 受け口の終了状態(spec 3.3 の表、3.2.2)。
const (
	RecvExitDenied      = 126 // 拒否(許可リスト外・対象 id 違い・read-batch の入力の誤り)。補助コマンドも同じ
	RecvExitUnreachable = 255 // 核が受け口に届かない、または終了状態を得る前に接続が切れた
)

// RecvOps は補助コマンド SITATE_RECV が受ける操作(selftest と survey は含まない)。
var RecvOps = []string{"list", "read-batch", "dump-pg-globals", "dump-pg", "dump-my"}

// ダンプの置き場の固定名(spec 3.1.2.4)。<to>/<target-id>/ の下に置く。
const (
	DumpFileCustom  = "db.dump"
	DumpFileGlobals = "globals.sql"
)

// read-batch の変化の一覧(spec 3.3)。tar ストリーム(終端の 2 ブロックの後に、tar が record を満たすゼロ埋めが続いてよい。読む側が読み飛ばす)の後ろに
// 区切りの行 ReadBatchChangedMarker と、変化した相対パスを NUL で終えて並べた列が来て、標準出力が閉じて終わる。
const ReadBatchChangedMarker = "SITATE-CHANGED/1\n"
