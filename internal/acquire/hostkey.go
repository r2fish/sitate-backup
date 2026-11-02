package acquire

// 接続先の読み方と、照らすホスト鍵の値の選択(spec 3.3「ホスト鍵」、7.5 Surveyor.SelectHostKey)。

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 照らす時機(HostKeyChoiceInput.Timing の値)。
const (
	TimingSurvey = "survey"
	TimingEnroll = "enroll"
	TimingApply  = "apply"
)

// pullPort は pull 接続のポート(v1 は 22 固定。spec 3.3)。
const pullPort = 22

// sentinelFP はどの実際の指紋とも一致しない固定値。接続して見えた指紋だけを知りたいとき(何も実行せずに)、
// SSHConn.PinnedFP に置く(合わなければ SSH は何も実行せず見えた指紋を返す)。
const sentinelFP = "SHA256:" + "0000000000000000000000000000000000000000000"

// hostSpec は --host の値 ssh://<user>@<host>:<port> を読んだもの。Host は source.host と同じ形(IPv6 の括弧を外した形)。
type hostSpec struct {
	User, Host string
	Port       int
}

func parseHostArg(v string) (hostSpec, *contract.Error) {
	bad := errArg("--host", "format")
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "ssh" || u.Hostname() == "" || u.User == nil || u.User.Username() == "" || u.Path != "" && u.Path != "/" {
		return hostSpec{}, bad
	}
	port := pullPort
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return hostSpec{}, bad
		}
		port = n
	}
	h := hostSpec{User: u.User.Username(), Host: u.Hostname(), Port: port}
	if h.Port != pullPort {
		return hostSpec{}, errArg("--host", "unsupported_port")
	}
	return h, nil
}

// hostKeyMismatchCompare は選択の結果が「照らさない」か「照らす」かを使う側が読むための型。
func choose(in contract.HostKeyChoiceInput) contract.HostKeyChoice {
	first := func(l []string) string {
		if len(l) > 0 {
			return l[0]
		}
		return ""
	}
	switch in.Timing {
	case TimingSurvey:
		if in.Sealed {
			if fp := first(in.HostKeys); fp != "" {
				return contract.HostKeyChoice{Compare: true, FP: fp}
			}
		}
		return contract.HostKeyChoice{}
	case TimingEnroll:
		if in.Host != in.DefHost {
			return contract.HostKeyChoice{Err: errDefInvalid("/source/host", "invalid_value")}
		}
		if fp := first(in.SourceSeen); fp != "" {
			return contract.HostKeyChoice{Compare: true, FP: fp}
		}
		if fp := first(in.HostKeys); fp != "" {
			return contract.HostKeyChoice{Compare: true, FP: fp}
		}
		return contract.HostKeyChoice{Err: mkErr(contract.CodeSourceHostkeyMismatch, map[string]any{"expected_fp": nil, "session_id": nil},
			nextOf(contract.CodeSourceHostkeyMismatch, false, "source", "survey"))}
	case TimingApply:
		if in.ProposalFP == "" {
			return contract.HostKeyChoice{Err: errInternal("")}
		}
		return contract.HostKeyChoice{Compare: true, FP: in.ProposalFP}
	}
	return contract.HostKeyChoice{Err: errInternal("")}
}

// SelectHostKey は contract.Surveyor を満たす(状態ファイルへは書かない)。
func (*Surveyor) SelectHostKey(_ context.Context, in contract.HostKeyChoiceInput) (contract.HostKeyChoice, error) {
	return choose(in), nil
}

// withSeen は expected_fp が null の SOURCE_HOSTKEY_MISMATCH に、見えた指紋を足す。
func withSeen(e *contract.Error, seen string) *contract.Error {
	if e == nil || e.Code != contract.CodeSourceHostkeyMismatch {
		return e
	}
	return errHostKey("", seen)
}

func trimHost(h string) string { return strings.Trim(h, "[]") }
