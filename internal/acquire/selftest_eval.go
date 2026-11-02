package acquire

// 受け口の selftest の出力(sitate.recv-selftest/v1)から selftest[] の項目を作る(spec 3.3「selftest[] の項目」)。
// 外れた項目は誤りでなく項目の ok: false と code で返す(単独の source check は失敗の封筒でなく結果に載せる)。

import (
	"bytes"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type selfTarget struct {
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	Root             string  `json:"root"`
	RootState        string  `json:"root_state"`
	DB               string  `json:"db"`
	SocketDir        string  `json:"socket_dir"`
	Port             int     `json:"port"`
	Connect          bool    `json:"connect"`
	ConnectError     *string `json:"connect_error"`
	ServerVersionNum *int    `json:"server_version_num"`
	PgDumpVersionNum *int    `json:"pg_dump_version_num"`
	UnreadableTables *int    `json:"unreadable_tables"`
	LargeObjects     *bool   `json:"large_objects"`
}

type selfDoc struct {
	ReceiverHash   string `json:"receiver_hash"`
	AuthorizedKeys struct {
		Lines int  `json:"lines"`
		Match bool `json:"match"`
	} `json:"authorized_keys"`
	Targets []selfTarget `json:"targets"`
}

// selfItem は selftest[] の 1 項目(schema/cli/results.json の selftest)。
type selfItem struct {
	Name             string   `json:"name"`
	TargetID         *string  `json:"target_id"`
	OK               bool     `json:"ok"`
	Code             *string  `json:"code"`
	Unreadable       []string `json:"unreadable"`
	UnreadableTables *int     `json:"unreadable_tables"`
	LargeObjects     *bool    `json:"large_objects"`
	setup            *setupDetail
}

func parseSelfDoc(out []byte) (selfDoc, error) {
	var d selfDoc
	out = bytes.TrimSpace(out)
	if contract.Validate("receiver/selftest.json", out) != nil || json.Unmarshal(out, &d) != nil {
		return d, errInternal("output")
	}
	return d, nil
}

func newItem(name string, tid *string, ok bool, code contract.Code) selfItem {
	it := selfItem{Name: name, TargetID: tid, OK: ok, Unreadable: []string{}}
	if !ok {
		c := string(code)
		it.Code = &c
	}
	return it
}

// evalSelftest は共通の項目(source enroll と source check の両方で行うもの)を作る。
func evalSelftest(d selfDoc, wantHash string, def checkDef) []selfItem {
	sf := contract.CodeReceiverSelftestFailed
	items := []selfItem{
		newItem("from_restricted", nil, d.AuthorizedKeys.Match, sf),
		newItem("single_key_line", nil, d.AuthorizedKeys.Lines == 1, sf),
		newItem("receiver_hash", nil, d.ReceiverHash == wantHash, sf),
	}
	items = append(items, targetsMatch(d, def)...)
	for _, t := range d.Targets {
		if t.Kind == "postgresql" && inDef(def, t.ID) {
			items = append(items, dbItems(t)...)
		}
	}
	return items
}

func inDef(def checkDef, id string) bool {
	for _, t := range def.Targets {
		if t.ID == id {
			return true
		}
	}
	return false
}

// targetsMatch は定義の対象ごとに許可リストの要素が同じであること(root_state が ok)を確かめる。定義に無い要素はその対象 id で外れる。
func targetsMatch(d selfDoc, def checkDef) []selfItem {
	mm := contract.CodeReceiverTargetsMismatch
	by := map[string]selfTarget{}
	for _, t := range d.Targets {
		by[t.ID] = t
	}
	var items []selfItem
	for _, t := range def.Targets {
		id := t.ID
		it := newItem("targets_match", &id, true, mm)
		got, ok := by[t.ID]
		switch {
		case !ok:
			it = newItem("targets_match", &id, false, mm)
		case t.Files != "":
			if got.Kind != "files" || got.Root != t.Files {
				it = newItem("targets_match", &id, false, mm)
			} else if got.RootState != "ok" {
				it = newItem("targets_match", &id, false, mm)
				it.Unreadable = []string{got.Root}
			}
		default:
			if got.Kind != "postgresql" || got.DB != t.Postgresql || (t.Port != nil && *t.Port != got.Port) {
				it = newItem("targets_match", &id, false, mm)
			}
		}
		items = append(items, it)
		delete(by, t.ID)
	}
	for _, t := range d.Targets {
		if _, extra := by[t.ID]; extra {
			id := t.ID
			items = append(items, newItem("targets_match", &id, false, mm))
		}
	}
	return items
}

func dbItems(t selfTarget) []selfItem {
	id := t.ID
	sc := contract.CodeSourceSetupFailed
	conn := newItem("db_connect", &id, t.Connect, sc)
	if !t.Connect {
		d := setupDetail{Step: "selftest", TargetID: &id, Reason: "db_connect"}
		if t.ConnectError != nil && *t.ConnectError == "connect_privilege" {
			d.Reason, d.Lacks = "db_privileges", []string{"connect"}
		}
		conn.setup = &d
		return []selfItem{conn}
	}
	un := 0
	if t.UnreadableTables != nil {
		un = *t.UnreadableTables
	}
	priv := newItem("db_privileges", &id, un == 0, sc)
	priv.UnreadableTables, priv.LargeObjects = &un, t.LargeObjects
	if un != 0 {
		priv.setup = &setupDetail{Step: "selftest", TargetID: &id, Reason: "db_privileges", Lacks: []string{"tables"}, Unreadable: &un}
	}
	toolsOK := t.ServerVersionNum != nil && t.PgDumpVersionNum != nil && *t.PgDumpVersionNum/10000 >= *t.ServerVersionNum/10000
	tools := newItem("db_tools", &id, toolsOK, sc)
	if !toolsOK {
		tools.setup = &setupDetail{Step: "selftest", TargetID: &id, Reason: "db_tools", ServerVer: t.ServerVersionNum, DumpVer: t.PgDumpVersionNum}
	}
	return []selfItem{conn, priv, tools}
}

// itemsError は外れた項目から単独の呼び出し(source enroll)が返す誤りを作る。外れが無ければ nil。
// RECEIVER_SELFTEST_FAILED を先に、次に RECEIVER_TARGETS_MISMATCH、最後に SOURCE_SETUP_FAILED(selftest)。
func itemsError(items []selfItem, sealed bool) *contract.Error {
	var failed, ids []string
	var setup *setupDetail
	for _, it := range items {
		if it.OK {
			continue
		}
		switch contract.Code(*it.Code) {
		case contract.CodeReceiverSelftestFailed:
			failed = append(failed, it.Name)
		case contract.CodeReceiverTargetsMismatch:
			ids = append(ids, *it.TargetID)
		default:
			if setup == nil && it.setup != nil {
				setup = it.setup
			}
		}
	}
	switch {
	case len(failed) > 0:
		return mkErr(contract.CodeReceiverSelftestFailed, map[string]any{"failed": failed}, nextOf(contract.CodeReceiverSelftestFailed, false, "source", "enroll"))
	case len(ids) > 0:
		nx := nextOf(contract.CodeReceiverTargetsMismatch, false, "source", "enroll")
		if sealed {
			nx = nextOf(contract.CodeReceiverTargetsMismatch, false, "propose", "source-change")
		}
		return mkErr(contract.CodeReceiverTargetsMismatch, map[string]any{"target_ids": ids}, nx)
	case setup != nil:
		return errSetup(*setup)
	}
	return nil
}
