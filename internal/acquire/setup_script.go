package acquire

// source-setup の台本の生成(spec 3.3「source-setup の実行と入出力」)。値はすべて台本の中に単一引用符の文字列として埋め込み、
// 引数・環境変数・別のファイルでは渡さない。埋め込む変数(U2a の source-setup の冒頭のコメントが正):
//
//	SITATE_CLUSTERS 信じるクラスタの一覧(1 変数に 1 クラスタ 1 行。socket_dir・port・major・raw_path... をタブで区切る。contract.EmbedVarClusters)
//	SITATE_TARGETS  許可リスト(1 対象 1 行、タブ区切りで空の欄を置かない)
//	                  ファイル  : <id> TAB files TAB <根> TAB <根の JSON 文字列>
//	                  PostgreSQL: <id> TAB postgresql TAB <DB 名> TAB <ソケット> TAB <ポート> TAB <DB 名の JSON> TAB <ソケットの JSON> TAB <port 指定あり 1|0>
//	SITATE_PULL_KEY pull 用公開鍵(authorized_keys の鍵の部分: `ssh-ed25519 AAAA...`)
//	SITATE_FROM     authorized_keys の from= に書くバックアップサーバーのアドレス(Acquire.From・SourceEnroller.From。空なら台本が SSH_CLIENT から取る)
//	SITATE_RECEIVER 受け口の中身(sitate-recv の BEGIN-EMBEDDED〜END-EMBEDDED の行を核が置き換えた後のバイト列そのもの)。receiver_hash はこの SHA-256
//
// 受け口に埋めるもの: SITATE_VERSION(Version)、SITATE_AUTH_LINE(authorized_keys の行)、SITATE_TARGETS(許可リスト)、SITATE_AUTHKEYS_FILE(既定のまま)。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/r2fish/sitate-backup/adapters/acquire/recv"
	"github.com/r2fish/sitate-backup/internal/contract"
)

func quoteAny(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// quoteLine は 1 行の値(改行・NUL を含められない)を引用する。タブ区切りの欄の中のタブも入れられない。
func quoteLine(fields ...string) (string, error) {
	for _, f := range fields {
		if strings.ContainsAny(f, "\x00\n\t") {
			return "", fmt.Errorf("埋め込めない値")
		}
	}
	return quoteAny(strings.Join(fields, "\t")), nil
}

func clusterRows(cl []stateCluster) (string, error) {
	if len(cl) == 0 {
		return "", nil
	}
	lines := make([]string, 0, len(cl))
	for _, c := range cl {
		f := append([]string{c.SocketDir, fmt.Sprint(c.Port), fmt.Sprint(c.Major)}, c.RawPaths...)
		for _, x := range f {
			if strings.ContainsAny(x, "\x00\n\t") {
				return "", fmt.Errorf("埋め込めない値")
			}
		}
		lines = append(lines, strings.Join(f, "\t"))
	}
	// 1 変数に 1 クラスタ 1 行で入れる(sh は同じ変数への代入を重ねると後が前を消す)。
	return contract.EmbedVarClusters + "=" + quoteAny(strings.Join(lines, "\n")) + "\n", nil
}

// Version は受け口に埋める核の版(SITATE_VERSION。[0-9A-Za-z.+-]{1,64})。cli がビルドの版を入れる。
var Version = "0.1.0-dev"

const authKeysFile = "/var/lib/sitate-ro/.ssh/authorized_keys"

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }

// targetsValue は許可リスト(行は改行区切り)。PostgreSQL の対象のクラスタは、port の指定があればそのポート、無ければ信じるクラスタが 1 つだけのときそれ。
func targetsValue(def checkDef, clusters []stateCluster) (string, error) {
	var rows []string
	for _, t := range def.Targets {
		f := []string{t.ID, "files", t.Files, jsonStr(t.Files)}
		if t.Postgresql != "" {
			cl, err := pickCluster(t, clusters)
			if err != nil {
				return "", err
			}
			has := "0"
			if t.Port != nil {
				has = "1"
			}
			f = []string{t.ID, "postgresql", t.Postgresql, cl.SocketDir, fmt.Sprint(cl.Port), jsonStr(t.Postgresql), jsonStr(cl.SocketDir), has}
		}
		for _, x := range f {
			if x == "" || strings.ContainsAny(x, "\x00\n\t") {
				return "", errDefInvalid("/source/targets", "invalid_value")
			}
		}
		rows = append(rows, strings.Join(f, "\t"))
	}
	return strings.Join(rows, "\n"), nil
}

// pickCluster は DB の対象のクラスタを決める。主版 13 未満は選ばない(server_too_old)。
func pickCluster(t checkTarget, cl []stateCluster) (stateCluster, error) {
	fail := func(reason string) error {
		id := t.ID
		return errSetup(setupDetail{Step: "db_role", TargetID: &id, Reason: reason})
	}
	var cand []stateCluster
	for _, c := range cl {
		if t.Port == nil || *t.Port == c.Port {
			cand = append(cand, c)
		}
	}
	switch {
	case len(cand) == 0:
		return stateCluster{}, fail("cluster_not_found")
	case len(cand) > 1:
		return stateCluster{}, fail("cluster_ambiguous")
	case cand[0].Major < contract.PGMajorMin:
		return stateCluster{}, fail("server_too_old")
	}
	return cand[0], nil
}

// embedReceiver は受け口の BEGIN-EMBEDDED〜END-EMBEDDED の行を、埋め込む値の代入に置き換える。
func embedReceiver(src, targets, authLine string) (string, error) {
	const begin, end = "# BEGIN-EMBEDDED\n", "# END-EMBEDDED\n"
	i, j := strings.Index(src, begin), strings.Index(src, end)
	block := "SITATE_VERSION=" + quoteAny(Version) + "\nSITATE_AUTH_LINE=" + quoteAny(authLine) + "\nSITATE_TARGETS=" + quoteAny(targets) +
		"\nSITATE_AUTHKEYS_FILE=" + quoteAny(authKeysFile) + "\n"
	if i < 0 || j < i { // 印の無い骨格の受け口: 末尾に置く(U2a の実物には印がある)
		return src + begin + block + end, nil
	}
	return src[:i+len(begin)] + block + src[j:], nil
}

// setupScript は台本と receiver_hash を作る。from は authorized_keys の from= に書くアドレス(空なら台本が SSH_CLIENT から取る)。
func setupScript(def checkDef, clusters []stateCluster, pubKeyLine, from string) (script []byte, receiverHash string, err error) {
	return setupScriptFrom(recv.Files.ReadFile, def, clusters, pubKeyLine, from)
}

// setupScriptFrom は受け口とスクリプトの本体の読み出し元を差し替えられる(試験が別の版の実物を流す)。
func setupScriptFrom(read func(string) ([]byte, error), def checkDef, clusters []stateCluster, pubKeyLine, from string) (script []byte, receiverHash string, err error) {
	rcvSrc, e1 := read("sitate-recv")
	body, e2 := read("source-setup")
	if e1 != nil || e2 != nil {
		return nil, "", errInternal("")
	}
	cr, e1 := clusterRows(clusters)
	if e1 != nil {
		return nil, "", errDefInvalid("/source/targets", "invalid_value")
	}
	targets, err := targetsValue(def, clusters)
	if err != nil {
		return nil, "", err
	}
	auth := `restrict,from="` + from + `",command="/usr/local/libexec/sitate-recv" ` + pubKeyLine
	rcv, err := embedReceiver(string(rcvSrc), targets, auth)
	if err != nil {
		return nil, "", err
	}
	h := sha256.Sum256([]byte(rcv))
	var b strings.Builder
	b.WriteString(cr)
	b.WriteString("SITATE_TARGETS=" + quoteAny(targets) + "\n")
	b.WriteString("SITATE_PULL_KEY=" + quoteAny(pubKeyLine) + "\n")
	if from != "" {
		b.WriteString("SITATE_FROM=" + quoteAny(from) + "\n")
	}
	b.WriteString("SITATE_RECEIVER=" + quoteAny(rcv) + "\n")
	b.Write(body)
	return []byte(b.String()), "sha256:" + hex.EncodeToString(h[:]), nil
}
