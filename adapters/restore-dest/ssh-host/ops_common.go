package main

import (
	"context"
	"regexp"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

var pubKeyRe = regexp.MustCompile(`^ssh-ed25519 [A-Za-z0-9+/]+={0,2}$`)

// dedicated は start・stop・inspect が使う専用ユーザーの接続。pin は登録時に固定したホスト鍵の指紋(必須)。
// 秘密鍵は枠 restore-dest-ssh の値だけ。要求の不備は INTERNAL(request)で、何にも接続しない。
func dedicated(req *contract.RestoreDestRequest, pin *string) (*conn, *adapterErr) {
	t, ok := parseTarget(req.Dest.At)
	if !ok || pin == nil || *pin == "" {
		return nil, internalErr("request")
	}
	auth, ok := authFromSecret(req.Secrets["restore-dest-ssh"], false)
	if !ok {
		return nil, internalErr("request")
	}
	return dial(context.Background(), t, restoreUser, auth, *pin)
}

// remoteLines は出力を空白区切りの行に分ける。
func remoteLines(out string) [][]string {
	var rows [][]string
	for _, ln := range strings.Split(out, "\n") {
		if f := strings.Fields(ln); len(f) > 0 {
			rows = append(rows, f)
		}
	}
	return rows
}

func resRefs(rs []contract.AdapterResource) []contract.AdapterResource {
	if rs == nil {
		return []contract.AdapterResource{}
	}
	return rs
}
