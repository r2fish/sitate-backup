package contract

// 容器の補助コマンドの枠付け sitate.recvpipe/v1(spec 3.2.2)。形は schema/receiver/pipe.json の $comment のとおり。
const RecvPipeFormat = "sitate.recvpipe/v1"

// PipeRequest は補助コマンドが核へ送る最初の 1 行。
type PipeRequest struct {
	Format string `json:"format"`
	Op     string `json:"op"`
	Target string `json:"target"`
}
