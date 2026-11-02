package acquire

// Surveyor: 元サーバーの調査の実行と候補の判定(spec 3.3「元サーバーの調査」「候補の規則」、7.5)。状態ファイルへは書かない。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/r2fish/sitate-backup/adapters/acquire/recv"
	"github.com/r2fish/sitate-backup/internal/contract"
)

// Surveyor は contract.Surveyor の実物。SSH と Conn は調査の接続(Conn.PinnedFP が空でなければ見えた指紋を照らす)。
// SourceClusters は Judge が受け口の survey(sitate.recv-survey/v1)を判定するときの、状態ファイルの source_clusters の clusters 配列。
type Surveyor struct {
	SSH  contract.SSH
	Conn contract.SSHConn
	// Sudo は管理アクセスの <user> が root でないとき真(`sudo -n sh -s` で流す)。
	Sudo           bool
	SourceClusters json.RawMessage

	seen string
}

var _ contract.Surveyor = (*Surveyor)(nil)

// SeenFP は直前の接続で見えたホスト鍵の指紋(接続できなければ空)。
func (s *Surveyor) SeenFP() string { return s.seen }

// surveyBodyFile は調査の処理の本体(U2a が recv.Files に置く)。
const surveyBodyFile = "source-survey"

// surveyBodyFallback は recv.Files に本体がまだ無いときの置き換え(骨格の受け口と同じ形で NOT_IMPLEMENTED を返す)。
const surveyBodyFallback = "#!/bin/sh\necho \"NOT_IMPLEMENTED: source-survey\" >&2\nexit 1\n"

func shQuote(s string) (string, error) {
	if strings.ContainsAny(s, "\x00\n") {
		return "", errors.New("改行か NUL を含む値は埋め込めない")
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'", nil
}

// surveyScript は核が生成する調査の処理(範囲と du の打ち切りを先頭に埋め込む)。
func surveyScript(scope string) ([]byte, error) {
	if scope != contract.SurveyScopeFull && scope != contract.SurveyScopeDBOnly {
		return nil, errInternal("")
	}
	body, err := recv.Files.ReadFile(surveyBodyFile)
	if err != nil {
		body = []byte(surveyBodyFallback)
	}
	pre := fmt.Sprintf("%s='%s'\n%s='%d'\n", contract.EmbedVarScope, scope, contract.EmbedVarDuTimeout, contract.SurveyDuTimeoutS)
	return append([]byte(pre), body...), nil
}

// sshOut は 1 回の実行の標準出力と結果。
type sshOut struct {
	Out    []byte
	Exit   int
	SeenFP string
}

// runSSH は 1 回の実行。接続の失敗は seen を残して返す(型付きの誤りへの写像は呼び出し側)。
func runSSH(ctx context.Context, s contract.SSH, req contract.SSHRequest) (sshOut, error) {
	var buf bytes.Buffer
	req.Stdout = &buf
	r, err := s.Run(ctx, req)
	return sshOut{Out: buf.Bytes(), Exit: r.ExitStatus, SeenFP: r.SeenFP}, err
}

// mapConnErr は接続の失敗を原因コードに写す。auth と key_unparsable は mkAuth が呼び出し側の意味(調査 / 準備)で作る。
func mapConnErr(err error, mkAuth func(reason string) *contract.Error) error {
	var hk *contract.SSHHostKeyError
	if errors.As(err, &hk) {
		return errHostKey(hk.Pinned, hk.Seen)
	}
	var se *contract.SSHError
	if errors.As(err, &se) {
		switch se.Reason {
		case contract.SSHReasonAuth, contract.SSHReasonKeyUnparsable:
			return mkAuth(se.Reason)
		}
		return errUnreachable()
	}
	var ce *contract.Error
	if errors.As(err, &ce) {
		return ce
	}
	return errUnreachable()
}

// Survey は contract.Surveyor を満たす。流すのは核が生成した固定の処理だけ。
func (s *Surveyor) Survey(ctx context.Context, scope string) (json.RawMessage, error) {
	script, err := surveyScript(scope)
	if err != nil {
		return nil, err
	}
	if s.SSH == nil {
		return nil, errInternal("")
	}
	if e := ctxErr(ctx); e != nil {
		return nil, e
	}
	o, err := runSSH(ctx, s.SSH, contract.SSHRequest{SSHConn: s.Conn, Script: script, Sudo: s.Sudo})
	s.seen = o.SeenFP
	if err != nil {
		return nil, mapConnErr(err, func(r string) *contract.Error { return errSurveyFailed("connect", r) })
	}
	return parseSurvey(o, s.Sudo)
}

// parseSurvey は調査の処理の終了状態と出力を読む(失敗の読み替えは spec 3.3「失敗の判定」)。
func parseSurvey(o sshOut, sudo bool) (json.RawMessage, error) {
	if o.Exit == contract.SurveyExitUnsupportedOS {
		return nil, errSurveyFailed("prereq", "unsupported_os")
	}
	if sudo && len(bytes.TrimSpace(o.Out)) == 0 && (o.Exit == 1 || o.Exit == 127) {
		return nil, errSurveyFailed("sudo", "sudo_unavailable")
	}
	if o.Exit != 0 || contract.Validate("receiver/survey.json#/$defs/source_survey", bytes.TrimSpace(o.Out)) != nil {
		return nil, errInternal("output")
	}
	return json.RawMessage(bytes.TrimSpace(o.Out)), nil
}
