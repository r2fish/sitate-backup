package main

import "github.com/r2fish/sitate-backup/internal/contract"

// opStop は平文の消去とその確かめ(spec 3.2.4 の「消去」)。対象は session_id の物だけで、要求の resources[] の id では決めない。
// session_id が run_id の形でない(空を含む)か、workdir・scratch の id が session_id と違う要求は、何も触らずに INTERNAL(request)。
func opStop(req *contract.RestoreDestRequest) (map[string]any, *adapterErr) {
	if req.SessionID == nil || !validSessionID(*req.SessionID) || !resourcesConsistent(*req.SessionID, req.Resources) {
		return nil, internalErr("request")
	}
	c, e := dedicated(req, req.HostKeyFP)
	if e != nil {
		return nil, e
	}
	defer c.close()
	r, err := c.run(teardownScript, "", *req.SessionID)
	if err != nil {
		return nil, unreachableErr("connect")
	}
	if r.code == 64 {
		return nil, internalErr("request")
	}
	if r.code != 0 {
		return nil, customErr("TEARDOWN_FAILED")
	}
	rem := []contract.AdapterResource{}
	for _, f := range remoteLines(r.out) {
		if len(f) == 3 && f[0] == "rem" {
			rem = append(rem, contract.AdapterResource{Type: f[1], ID: f[2]})
		}
	}
	return map[string]any{"done": len(rem) == 0, "verified_by": "ssh", "remaining": rem}, nil
}
