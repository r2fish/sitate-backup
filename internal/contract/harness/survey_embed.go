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

// SetupPreamble は source-setup の台本の先頭に置くクラスタの一覧。1 変数に 1 クラスタ 1 行で入れる(sh は同じ変数への代入を重ねると後が前を消す)。
func SetupPreamble(cl []StateCluster) (string, error) {
	if len(cl) == 0 {
		return "", nil
	}
	lines := make([]string, 0, len(cl))
	for _, c := range cl {
		f := append([]string{c.SocketDir, fmt.Sprint(c.Port), fmt.Sprint(c.Major)}, c.RawPaths...)
		for _, x := range f {
			if strings.ContainsAny(x, "\t\n") {
				return "", fmt.Errorf("タブと改行を含む値は埋め込めない")
			}
			if _, err := shQuote(x); err != nil {
				return "", err
			}
		}
		lines = append(lines, strings.Join(f, "\t"))
	}
	return contract.EmbedVarClusters + "='" + strings.ReplaceAll(strings.Join(lines, "\n"), "'", `'\''`) + "'\n", nil
}
