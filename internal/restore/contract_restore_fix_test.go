package restore

// 契約試験の準備(偽の保管先に世代を置く、状態の世代の一覧を作る、台帳と定義を置く)。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

const (
	refA = "m-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	refB = "m-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	refC = "m-cccccccccccccccccccccccccccccccc"
	refD = "m-dddddddddddddddddddddddddddddddd"
)

func ts(t time.Time) string { return t.UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z") }

// snap は世代の一覧(状態の snapshots[])の 1 要素の材料。
type snap struct {
	Ref, Class, Health       string
	Created                  time.Time
	TargetsOK, RestoreFailed bool
	RestoreVerified          *bool
	Stored                   time.Time
	Body                     string // 保管先に置くマニフェストの本体(空なら仮の文)
	// 保管先が Put の応答で返した値(seed が埋める。世代の記録はこの値を持つ)。
	VerID, ETag string
	Size        int64
	Retain      time.Time
}

func bptr(b bool) *bool { return &b }

func (s snap) json() map[string]any {
	return map[string]any{"ref": s.Ref, "version_id": s.VerID, "size": s.Size, "etag": s.ETag,
		"retain_until": ts(s.Retain), "created_at": ts(s.Created), "stored_at": ts(s.Stored),
		"retention_class": s.Class, "retention_until": ts(s.Stored.AddDate(0, 0, 90)), "health": s.Health, "targets_ok": s.TargetsOK,
		"restore_verified": s.RestoreVerified, "refs_verified": nil, "restore_failed": s.RestoreFailed, "def_copy_mismatch": false,
		"baseline": nil, "format_version": 1}
}

// seed は偽の保管先へ manifests/<ref> を gap 刻みの時刻で書き、状態へ世代の一覧を置く。snaps[i].Stored は保管先の書き込み時刻で埋まる。
func seed(t *testing.T, st *fake.StateFiles, clk *fake.ManualClock, core *fake.Core, snaps []snap, gap time.Duration) {
	t.Helper()
	ws := &fake.ObjectStore{Core: core, Bucket: "b", Role: fake.RoleWrite, Params: contract.FixedRetention{}}
	// 版 ID は保管先の応答の値を記録する(spec 4.2「記録した版 ID を必ず指定する」)。作り物の版 ID は使わない。
	var arr []map[string]any
	for i := range snaps {
		clk.Advance(gap)
		class := contract.RetentionClass(snaps[i].Class)
		body := snaps[i].Body
		if body == "" {
			body = "manifest-" + snaps[i].Ref
		}
		pr, err := ws.Put(ctx, "manifests/"+snaps[i].Ref, strings.NewReader(body), class)
		if err != nil {
			t.Fatal(err)
		}
		snaps[i].VerID, snaps[i].ETag, snaps[i].Size, snaps[i].Retain = pr.VersionID, pr.ETag, pr.Size, pr.RetainUntil
		snaps[i].Stored = clk.T
		arr = append(arr, snaps[i].json())
	}
	b, _ := json.Marshal(arr)
	for _, e := range arr {
		eb, _ := json.Marshal(e)
		if err := contract.Validate("data/state.json#/$defs/snapshot", eb); err != nil {
			t.Fatalf("準備の世代の要素が状態のスキーマに合わない: %v", err)
		}
	}
	if st.Items == nil {
		st.Items = map[string]json.RawMessage{}
	}
	st.Items["initialized"] = json.RawMessage("true")
	st.Items["snapshots"] = b
}

// 復元先が ssh-host の承認済みの定義(restore 節)。
const defSSH = `{"version":1,"restore":{"adapter":"ssh-host","dest":{"at":"ssh://restore@dest.example.invalid:22","key_ref":"dest-key"}}}`

// ledgerSession は破棄を確かめられず残った復元テストの資源の台帳の記録(状態の台帳のスキーマで検査する)。
func ledgerSession(t *testing.T, runID, session string, remaining bool) json.RawMessage {
	t.Helper()
	res := []map[string]string{}
	var td any
	if remaining {
		res = append(res, map[string]string{"type": "workdir", "id": session})
		td = map[string]any{"remaining": res, "first_failed_at": "2027-02-27T00:00:00Z"}
	}
	rec := map[string]any{"record_id": "l-0123456789abcdef", "kind": "dest_session", "run_id": runID, "adapter": "ssh-host",
		"dest": map[string]any{"at": destURL, "key_ref": "dest-key"}, "purpose": "restore_test", "session_id": session, "state": "destroy_failed", "resources": res,
		"ssh": map[string]any{"host": "dest.example.invalid", "port": 22, "user": "sitate-restore"}, "host_key_fp": "SHA256:abcDEF123",
		"backup_ref": refA, "paths": nil, "expires_at": nil, "teardown": td}
	b, _ := json.Marshal(rec)
	if err := contract.Validate("data/state.json#/$defs/ledger", b); err != nil {
		t.Fatalf("準備の台帳の記録が状態のスキーマに合わない: %v", err)
	}
	return b
}

func resultOf(t *testing.T, rep contract.Reply, schemaDef string) map[string]json.RawMessage {
	t.Helper()
	if err := contract.Validate("cli/results.json#/$defs/"+schemaDef, rep.Result); err != nil {
		t.Fatalf("結果が results.json の %s に合わない: %v\n%s", schemaDef, err, rep.Result)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(rep.Result, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func jstr(t *testing.T, m map[string]json.RawMessage, k string) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(m[k], &s); err != nil {
		t.Fatalf("%s が文字列でない: %s", k, m[k])
	}
	return s
}

func wantRaw(t *testing.T, m map[string]json.RawMessage, k, want string) {
	t.Helper()
	if string(m[k]) != want {
		t.Errorf("%s は %s を期待したが %s", k, want, m[k])
	}
}

// sshSecret は kind: ssh_key の秘密ファイルの値(spec 4.5 の表。private_key・public_key・fp)。
func sshSecret() json.RawMessage {
	b, _ := json.Marshal(contract.SSHKeySecret{Kind: contract.SecretKindSSHKey, PrivateKey: privKey, PublicKey: "ssh-ed25519 AAAAfakepub", Fp: fpDest})
	return b
}

// storageSecret は kind: storage_keys の秘密ファイルの値(書き込み鍵と読み取り鍵の組)。
func storageSecret() json.RawMessage {
	b, _ := json.Marshal(contract.StorageKeysSecret{Kind: contract.SecretKindStorageKeys, Keys: []contract.StorageKeyEntry{
		{Role: "write", KeyID: "kW", Secret: "sW"}, {Role: "read", KeyID: "kR", Secret: "sR"}}})
	return b
}

const destURL = "ssh://restore@dest.example.invalid:22"

// strictStore は版 ID なしの Get を拒む ObjectStore(記録した版 ID を必ず指定する。版を指定しない読み出しは T-14 の差し替えに弱い)。
type strictStore struct {
	contract.ObjectStore
	blind int
}

func (s *strictStore) Get(c context.Context, key, versionID string) (io.ReadCloser, error) {
	if versionID == "" {
		s.blind++
		return nil, errors.New("試験: 版 ID なしの読み出しは使わない(記録した版 ID を指定する)")
	}
	return s.ObjectStore.Get(c, key, versionID)
}
