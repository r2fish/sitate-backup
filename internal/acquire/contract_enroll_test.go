package acquire

// 契約試験(作業指示書 5.4 の 5): source enroll の順(spec 3.3「source enroll の順」)を、試験の中の ssh サーバーの実行の記録で確かめる。
// (1) ホスト鍵の照合 → (2) Surveyor の db_only → (3) クラスタを埋め込んだ source-setup の実行 → (4) 合格したら記録。
// 合格まで通す経路は、pull 用鍵の保存先(Secrets)が Acquire の公開フィールドに無く、ここでは確かめられない(足りない契約。報告)。

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

var enrollWords = []string{"source", "enroll"}

func enrollItems(extra map[string]json.RawMessage) map[string]json.RawMessage {
	m := map[string]json.RawMessage{"sealed": sealedItem(false)}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func majorPtr(n int) *int { return &n }

// dbOnlyDoc は信じるクラスタ 1 つ(readable なら読める)の db_only の調査の出力。
func dbOnlyDoc(t *testing.T, readable bool) string {
	c := harness.SurveyCluster{SocketDir: "/var/run/postgresql", Port: 5432, Major: majorPtr(16), RawPaths: []string{"/var/lib/postgresql/16/main"}, Readable: readable, NamesReadable: readable}
	if !readable {
		c.Major, c.RawPaths = nil, []string{}
	}
	return surveyDoc(t, harness.SourceSurveyIn{Clusters: []harness.SurveyCluster{c}}, contract.SurveyScopeDBOnly, nil)
}

// 定義の source.host と --host の <host> が(正規化した形で)違えば、何も流さず接続もせずに DEF_INVALID(/source/host)。
func TestContractAcquireSourceEnrollHostDiffers(t *testing.T) {
	src := startSource(t, nil)
	a, st := newSrcAcquire(enrollItems(map[string]json.RawMessage{"source_seen": seenItem("localhost", src.FP)}))
	url := "ssh://root@localhost:22"
	_, err := a.SourceEnroll(ctx, sourceCall(enrollWords, url, newAdminKey(t)))
	ce := harness.ExpectCode(t, err, contract.CodeDefInvalid)
	errEnvelope(t, ce)
	var d struct {
		Issues []struct{ Pointer, Reason string }
	}
	_ = json.Unmarshal(ce.Detail, &d)
	if len(d.Issues) != 1 || d.Issues[0].Pointer != "/source/host" || d.Issues[0].Reason != "invalid_value" {
		t.Errorf("issues は /source/host の invalid_value: %s", ce.Detail)
	}
	if len(src.Execs()) != 0 {
		t.Error("何も流さない")
	}
	if len(st.Items["host_keys"]) != 0 || len(st.Items["source_clusters"]) != 0 {
		t.Error("何も書かない")
	}
}

// 記録も固定値も無ければ最初の接続として受けず、SOURCE_HOSTKEY_MISMATCH(expected_fp: null)。何も流さず、書かない。
func TestContractAcquireSourceEnrollNoRecord(t *testing.T) {
	src := startSource(t, nil)
	a, st := newSrcAcquire(enrollItems(nil))
	_, err := a.SourceEnroll(ctx, sourceCall(enrollWords, src.URL("root"), newAdminKey(t)))
	ce := harness.ExpectCode(t, err, contract.CodeSourceHostkeyMismatch)
	var d map[string]json.RawMessage
	_ = json.Unmarshal(ce.Detail, &d)
	if v, ok := d["expected_fp"]; !ok || string(v) != "null" {
		t.Errorf("expected_fp は null: %s", ce.Detail)
	}
	if len(src.Execs()) != 0 || len(st.Items["host_keys"]) != 0 {
		t.Error("何も流さず、書かない")
	}
}

// 照らす値(source_seen、無ければ固定値)と見えた指紋が違えば、以降を流さずに SOURCE_HOSTKEY_MISMATCH(expected_fp は照らした値)。
func TestContractAcquireSourceEnrollHostKeyMismatch(t *testing.T) {
	_, perr := shapeAcquire().SourceEnroll(ctx, contract.Call{Words: enrollWords})
	harness.Implemented(t, perr) // 親にも未実装の印を残す(契約試験の集計は親ごとに原因を見る)
	cases := map[string]map[string]json.RawMessage{
		"source_seen":            {"source_seen": seenItem("127.0.0.1", fpA)},
		"固定値(source_seen が無いとき)": {"host_keys": hostKeysItem("127.0.0.1", fpA)},
		"source_seen が固定値より先":    {"source_seen": seenItem("127.0.0.1", fpA), "host_keys": hostKeysItem("127.0.0.1", fpB)},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			src := startSource(t, nil)
			a, st := newSrcAcquire(enrollItems(extra))
			_, err := a.SourceEnroll(ctx, sourceCall(enrollWords, src.URL("root"), newAdminKey(t)))
			ce := harness.ExpectCode(t, err, contract.CodeSourceHostkeyMismatch)
			errEnvelope(t, ce)
			dm := map[string]any{}
			_ = json.Unmarshal(ce.Detail, &dm)
			if dm["expected_fp"] != fpA || dm["seen_fp"] != src.FP {
				t.Errorf("expected_fp は照らした値、seen_fp は見えた指紋: %s", ce.Detail)
			}
			if len(src.Execs()) != 0 {
				t.Errorf("違えば以降を流さない: %d 回", len(src.Execs()))
			}
			if string(st.Items["host_keys"]) != string(extra["host_keys"]) || len(st.Items["source_clusters"]) != 0 {
				t.Error("違えば host_keys・source_clusters を書かない")
			}
		})
	}
}

// 順: ホスト鍵が合えば最初に流すのは範囲 db_only の調査。読めないクラスタが 1 つでもあれば、source-setup を流さずに
// SOURCE_SETUP_FAILED(step: prereq、reason: cluster_unreadable)。
func TestContractAcquireSourceEnrollUnreadableCluster(t *testing.T) {
	src := startSource(t, nil)
	src.Reply = surveyReply(dbOnlyDoc(t, false))
	a, st := newSrcAcquire(enrollItems(map[string]json.RawMessage{"source_seen": seenItem("127.0.0.1", src.FP)}))
	_, err := a.SourceEnroll(ctx, sourceCall(enrollWords, src.URL("root"), newAdminKey(t)))
	ce := harness.ExpectCode(t, err, contract.CodeSourceSetupFailed)
	errEnvelope(t, ce)
	var d struct{ Step, Reason string }
	_ = json.Unmarshal(ce.Detail, &d)
	if d.Step != "prereq" || d.Reason != "cluster_unreadable" {
		t.Errorf("detail は step=prereq reason=cluster_unreadable: %s", ce.Detail)
	}
	ex := src.Execs()
	if len(ex) != 1 {
		t.Fatalf("読めないクラスタで止まり、流すのは調査の 1 回だけ: %d 回", len(ex))
	}
	pre, _ := harness.SurveyPreamble(contract.SurveyScopeDBOnly, contract.SurveyDuTimeoutS)
	if !strings.HasPrefix(ex[0].Stdin, pre) {
		t.Errorf("最初に流すのは範囲 db_only の調査: %q", firstLine(ex[0].Stdin))
	}
	if len(st.Items["host_keys"]) != 0 || len(st.Items["source_clusters"]) != 0 {
		t.Error("止まれば host_keys・source_clusters を書かない")
	}
}

// (3) 信じるクラスタの一覧を埋め込んだ source-setup を流す。段 roots が root_forbidden を返したら DEF_INVALID(root_forbidden、対象の根の pointer)。
// 合格しなかったので、host_keys・source_clusters は書かない。
func TestContractAcquireSourceEnrollRootForbidden(t *testing.T) {
	src := startSource(t, nil)
	src.Reply = func(r execRec) (string, int) {
		if strings.HasPrefix(r.Stdin, contract.EmbedVarScope+"=") {
			return dbOnlyDoc(t, true), 0
		}
		return `{"step":"prereq","target_id":null,"result":"unchanged","reason":null,"missing":null}` + "\n" +
			`{"step":"roots","target_id":"t1","result":"failed","reason":"root_forbidden","missing":null}` + "\n", 1
	}
	a, st := newSrcAcquire(enrollItems(map[string]json.RawMessage{"source_seen": seenItem("127.0.0.1", src.FP)}))
	_, err := a.SourceEnroll(ctx, sourceCall(enrollWords, src.URL("root"), newAdminKey(t)))
	ce := harness.ExpectCode(t, err, contract.CodeDefInvalid)
	errEnvelope(t, ce)
	var d struct {
		Issues []struct{ Pointer, Reason string }
	}
	_ = json.Unmarshal(ce.Detail, &d)
	if len(d.Issues) != 1 || d.Issues[0].Pointer != "/source/targets/0/files" || d.Issues[0].Reason != "root_forbidden" {
		t.Errorf("issues は対象の根の root_forbidden: %s", ce.Detail)
	}
	ex := src.Execs()
	if len(ex) != 2 {
		t.Fatalf("流すのは調査(db_only)と source-setup の順に 2 回: %d 回", len(ex))
	}
	row, err := harness.SetupPreamble([]harness.StateCluster{{SocketDir: "/var/run/postgresql", Port: 5432, Major: 16, RawPaths: []string{"/var/lib/postgresql/16/main"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ex[1].Stdin, row) {
		t.Errorf("source-setup の台本にクラスタの一覧を埋め込む(期待する行 %q)", strings.TrimSpace(row))
	}
	if ex[1].Cmd != "sh -s" {
		t.Errorf("source-setup も root は `sh -s`: %q", ex[1].Cmd)
	}
	if len(st.Items["host_keys"]) != 0 || len(st.Items["source_clusters"]) != 0 {
		t.Error("合格しなければ host_keys・source_clusters を書かない")
	}
}
