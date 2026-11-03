package acquire

// D-0166: アダプタが ok を返してもエントリ(ファイルもダンプも)が 0 件の対象は、核が partial にして TARGET_EMPTY とする
// (全消去の後の世代を latest にしない)。partial なので index/files は置き換えない。

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

func TestIngestEmptyTargetIsPartialWithTargetEmpty(t *testing.T) {
	e := newIngestEnv(t, ingestDef)
	e.script(newFrames().dir(".").file("a", []byte("one")).end(false), okResult("ok"))
	if _, err := e.run(t); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(e.indexFile("t1"))
	e.script(newFrames().dir(".").end(false), okResult("ok")) // 根のディレクトリだけ。ファイルが 0 件
	r, err := e.run(t)
	if err != nil {
		t.Fatal(err)
	}
	tg := r.Targets[0]
	if tg.Status != "partial" || !tg.Empty {
		t.Errorf("0 件の対象: status=%s empty=%v", tg.Status, tg.Empty)
	}
	if after, _ := os.ReadFile(e.indexFile("t1")); !bytes.Equal(before, after) {
		t.Error("0 件の取得で index/files を置き換えた(全消去が基準になる)")
	}
}

// D-0166: ホスト鍵の取得は資格情報を使わず、何も実行せずに見えた指紋を返す。届かなければ接続の誤り。
func TestSurveyorFetchHostKeyUsesNoCredential(t *testing.T) {
	ssh := &fake.SSH{FP: "SHA256:seenByFetch"}
	fp, err := (&Surveyor{SSH: ssh}).FetchHostKey(ctx, "src.example.invalid", 22)
	if err != nil || fp != "SHA256:seenByFetch" {
		t.Fatalf("fp=%q err=%v", fp, err)
	}
	calls := ssh.Recorded()
	if len(calls) != 1 || calls[0].Cred != "" || len(calls[0].Req.Argv) != 0 || len(calls[0].Req.Script) != 0 {
		t.Errorf("資格情報か実行する物を渡した: %+v", calls)
	}
	if _, err := (&Surveyor{SSH: unreachableSSH{}}).FetchHostKey(ctx, "x", 22); err == nil {
		t.Error("届かないのに成功した")
	}
}

type unreachableSSH struct{}

func (unreachableSSH) Run(context.Context, contract.SSHRequest) (contract.SSHResult, error) {
	return contract.SSHResult{}, &contract.SSHError{Reason: contract.SSHReasonConnect}
}
