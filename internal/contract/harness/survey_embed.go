package harness

import (
	"fmt"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func shQuote(s string) (string, error) {
	if strings.ContainsAny(s, "\x00\n") {
		return "", fmt.Errorf("改行か NUL を含む値は埋め込めない")
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'", nil
}

// SurveyPreamble は調査の処理の先頭に置く代入の行(範囲と du の打ち切り秒)。
func SurveyPreamble(scope string, duTimeoutS int) (string, error) {
	if scope != contract.SurveyScopeFull && scope != contract.SurveyScopeDBOnly {
		return "", fmt.Errorf("範囲が不正")
	}
	if duTimeoutS < 1 {
		return "", fmt.Errorf("du の打ち切りは 1 秒以上(空にしない)")
	}
	return fmt.Sprintf("%s='%s'\n%s='%d'\n", contract.EmbedVarScope, scope, contract.EmbedVarDuTimeout, duTimeoutS), nil
}

// SetupPreamble は source-setup の台本の先頭に置くクラスタの行(1 クラスタ 1 行)。
func SetupPreamble(cl []StateCluster) (string, error) {
	var b strings.Builder
	for _, c := range cl {
		f := append([]string{c.SocketDir, fmt.Sprint(c.Port), fmt.Sprint(c.Major)}, c.RawPaths...)
		for _, x := range f {
			if strings.Contains(x, "\t") {
				return "", fmt.Errorf("タブを含む値は埋め込めない")
			}
		}
		q, err := shQuote(strings.Join(f, "\t"))
		if err != nil {
			return "", err
		}
		b.WriteString(contract.EmbedVarCluster + "=" + q + "\n")
	}
	return b.String(), nil
}
