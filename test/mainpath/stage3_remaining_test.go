//go:build mainpath

package mainpath

import (
	"encoding/json"
)

// recordEnrollAndRemaining は D-0159 の実機の確かめ(復元先 ssh-host の実物。受入試験の環境の dest)。
//   - 定義に restore.ssh_from が無いまま登録が通り、アダプタが同じ接続の SSH_CONNECTION の先頭(バックアップサーバーの送信元)を
//     受信許可にして結果の observed_from で返し、核が state.json の restore_dest_enrollment に記録したこと。
//   - 1 回の restore-test の終わりの stop の remaining(結果の teardown.remaining)の中身。逐語でログに残す(test/records/mainpath/ に写す)。
//   - 登録の後の dest は、受信をバックアップサーバーだけに絞る(隔離)ので、台本の端末からは dest の処理の一覧を取れない(記録の「未確認」)。
func (w *s2) recordEnrollAndRemaining(rt cliOut) {
	t := w.t
	var st struct {
		Enroll struct {
			Egress   []string `json:"egress_allow"`
			Observed *string  `json:"observed_from"`
		} `json:"restore_dest_enrollment"`
	}
	if err := json.Unmarshal([]byte(w.bak.root("cat "+stateDir+"/state.json")), &st); err != nil {
		t.Fatalf("state.json: %v", err)
	}
	if st.Enroll.Observed == nil || *st.Enroll.Observed != w.backupIP {
		t.Fatalf("restore_dest_enrollment.observed_from が、バックアップサーバーの送信元 %s と違う: %v", w.backupIP, st.Enroll.Observed)
	}
	t.Logf("restore_dest_enrollment: observed_from=%s egress_allow=%v", *st.Enroll.Observed, st.Enroll.Egress)
	var rr struct {
		Teardown *struct {
			Done      bool `json:"done"`
			Remaining []struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"remaining"`
		} `json:"teardown"`
	}
	if err := json.Unmarshal(rt.Env.Result, &rr); err != nil || rr.Teardown == nil {
		t.Fatalf("restore-test の結果に teardown が無い: %v %s", err, rt.Env.Result)
	}
	var rem []string
	for _, r := range rr.Teardown.Remaining {
		rem = append(rem, r.Type+":"+r.ID)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(rt.Env.Result, &raw)
	t.Logf("restore-test の teardown (逐語): %s", raw["teardown"])
	t.Logf("restore-test の teardown.done=%v remaining=%v", rr.Teardown.Done, rem)
	if !rr.Teardown.Done || len(rem) != 0 {
		t.Errorf("実物への 1 回の restore-test の後始末が残存なしで終わっていない: done=%v remaining=%v", rr.Teardown.Done, rem)
	}
}
