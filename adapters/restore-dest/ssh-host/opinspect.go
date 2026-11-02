package main

import (
	"github.com/r2fish/sitate-backup/internal/contract"
)

// opInspect は製品の印(~/.sitate/run/<session_id>/ とその session_id に結ばれた容器・処理)の付いた資源を列挙する。
// session_id が null なら全 session_id(掃引)。到達できないときは state: unknown(核は残存として扱う)。
// 印の無い物(run_id の形でない名前のディレクトリなど)は列挙しない。したがって核の掃引はそれを破棄の対象にできない。
func opInspect(req *contract.RestoreDestRequest) (map[string]any, *adapterErr) {
	want := ""
	if req.SessionID != nil {
		if !validSessionID(*req.SessionID) {
			return nil, internalErr("request")
		}
		want = *req.SessionID
	}
	c, e := dedicated(req, req.HostKeyFP)
	if e != nil {
		if e.code == "unreachable" {
			return map[string]any{"resources": []any{}, "state": "unknown"}, nil
		}
		return nil, e
	}
	defer c.close()
	r, err := c.run(inspectScript, "", want)
	if err != nil || r.code != 0 {
		return map[string]any{"resources": []any{}, "state": "unknown"}, nil
	}
	found := []map[string]any{}
	for _, f := range remoteLines(r.out) {
		if len(f) == 4 && f[0] == "res" && (want == "" || f[3] == want) {
			found = append(found, map[string]any{"type": f[1], "id": f[2], "session_id": f[3]})
		}
	}
	state := "absent"
	if len(found) > 0 {
		state = "present"
	}
	return map[string]any{"resources": found, "state": state}, nil
}
