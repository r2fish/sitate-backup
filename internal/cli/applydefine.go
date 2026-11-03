package cli

// apply の define 要素(spec 3.1.2.11「適用の操作の列」の define): 承認済みの定義を提案の定義へ切り替える。
// 書く順は config/<def_hash>(保管先。無ければ書く)→ StateFiles.SwitchDef(索引の削除・承認済みの定義の置き換え・host_keys[]・登録の結果)
// → 定義ファイルの書き戻し(def-change・seal など定義を渡す種別)。どの段も冪等で、再開は常に define から行う。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/state"
)

// DefFileDefault は定義ファイルの置き場(spec 3.6。seal と def-change の apply が書く)。
const DefFileDefault = "/etc/sitate/sitate.yaml"

// 節を変える種別 → 定義の節の名前(approval の kinds と同じ)。
var sectionOps = map[string]string{"restore-change": "restore", "source-change": "source", "notify-change": "notify", "notify-add": "notify"}

// 定義の全体を渡す種別(args.definition が正規化した定義)。
var definitionOps = map[string]bool{"seal": true, "def-change": true, "def-change-unlisted": true, "checks-weaken": true}

func (d *Deps) defFilePath() string {
	if d.DefFile != "" {
		return d.DefFile
	}
	return DefFileDefault
}

func (d *Deps) defineStep(ctx context.Context, ch contract.ChangeOps, st contract.OpStep, enroll *contract.EnrollResult) (json.RawMessage, error) {
	if d.State == nil {
		return nil, contract.ErrNotImplemented("cli.apply")
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(st.Args, &args) != nil {
		return nil, internalErr()
	}
	cur, curHash, _ := d.State.ReadDef(ctx) // seal の前は無い
	next, err := d.defAfter(ctx, ch.Op, args, cur)
	if err != nil {
		return nil, err
	}
	nextHash, err := contract.CanonicalHash(next)
	if err != nil {
		return nil, internalErr()
	}
	if curHash != nextHash { // 置き換えを飛ばすときは config/ を書き直さない
		if err := d.writeConfigCopy(ctx, nextHash, next); err != nil {
			return nil, err
		}
	}
	in := contract.SwitchInput{Normalized: next, HostKeys: hostKeyUpdates(ch.Op, args), Enroll: enroll}
	if err := d.State.SwitchDef(ctx, in); err != nil {
		return nil, err
	}
	if definitionOps[ch.Op] {
		if err := d.reregisterTimers(ctx); err != nil { // 定義の schedule・timezone が効く(冪等。失敗したら再開で登録し直す)
			return nil, err
		}
		if err := d.writeDefFile(ctx, next); err != nil {
			return nil, err
		}
	}
	return json.RawMessage(`{}`), nil
}

// reregisterTimers は承認済みの定義(切り替え後)の schedule・timezone でタイマーを登録し直す。Timers が無ければ何もしない。
func (d *Deps) reregisterTimers(ctx context.Context) error {
	if d.Timers == nil {
		return nil
	}
	tbl, err := contract.LoadTimerTable()
	if err != nil {
		return internalErr()
	}
	var units []contract.TimerUnit
	for _, u := range tbl.Units {
		for _, st := range u.Steps {
			units = append(units, contract.TimerUnit{Words: st.Args, Period: u.Trigger})
		}
	}
	if err := d.Timers.Register(ctx, units); err != nil {
		return err
	}
	return d.Timers.SetOuterLimit(ctx, contract.TimerSystemdTimeoutStart)
}

// defAfter は提案が承認された後の定義の正規化した値。全体を渡す種別は args.definition、節を変える種別は現在の定義のその節の置き換え、
// delay-change と restore-test-* は 1 項目の置き換え。
func (d *Deps) defAfter(ctx context.Context, op string, args map[string]json.RawMessage, cur []byte) ([]byte, error) {
	if definitionOps[op] {
		if len(args["definition"]) == 0 {
			return nil, internalErr()
		}
		return contract.CanonicalJSON(args["definition"])
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(cur, &doc) != nil || doc == nil {
		return nil, newErr(contract.CodeNotSealed, nil)
	}
	switch {
	case sectionOps[op] != "":
		doc[sectionOps[op]] = withoutHostKey(args[sectionOps[op]])
	case op == "delay-change":
		doc["approval_delay_hours"] = args["hours"]
	case op == "restore-test-reduce" || op == "restore-test-increase":
		doc["restore_test"] = args["to"]
	default:
		return nil, internalErr()
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, internalErr()
	}
	norm, _, issues, err := d.State.ValidateDef(ctx, b, true)
	if err != nil {
		return nil, err
	}
	if len(issues) > 0 && string(issues) != "[]" && string(issues) != "null" {
		return nil, newErr(contract.CodeDefInvalid, map[string]any{"issues": issues})
	}
	return norm, nil
}

// withoutHostKey は節から host_key_fp を除く(ホスト鍵の固定値は定義でなく state.json の host_keys[] に置く)。
func withoutHostKey(sec json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(sec, &m) != nil || m == nil {
		return sec
	}
	delete(m, "host_key_fp")
	delete(m, "admin_user") // 再登録に使う管理用の利用者は定義に入れない(D-0166)
	b, _ := json.Marshal(m)
	return b
}

// hostKeyUpdates は切り替えに渡すホスト鍵の更新の一覧。節に host_key_fp があるときだけ(作成 / 破棄の組や登録を伴わない変更は空)。
func hostKeyUpdates(op string, args map[string]json.RawMessage) []contract.HostKeyUpdate {
	var role, host string
	var sec struct {
		HostKeyFP string `json:"host_key_fp"`
		Host      string `json:"host"`
		Dest      struct {
			At string `json:"at"`
		} `json:"dest"`
	}
	switch op {
	case "source-change":
		role = "source"
		_ = json.Unmarshal(args["source"], &sec)
		host = sec.Host
	case "restore-change":
		role = "restore_dest"
		_ = json.Unmarshal(args["restore"], &sec)
		host = destHost(sec.Dest.At)
	default:
		return nil
	}
	if sec.HostKeyFP == "" {
		return nil
	}
	fp := sec.HostKeyFP
	return []contract.HostKeyUpdate{{Role: role, Host: host, HostKeyFP: &fp}}
}

// destHost は restore.dest.at のホスト名の部分(ssh://<user>@<host>:<port> の <host>。IPv6 の [...] を除く。spec 4.5 の host_keys[])。ssh:// でなければそのまま。
func destHost(at string) string {
	if !strings.HasPrefix(at, "ssh://") {
		return at
	}
	u, err := url.Parse(at)
	if err != nil || u.Hostname() == "" {
		return at
	}
	return u.Hostname()
}

// writeConfigCopy は保管先の config/<def_hash> を無ければ書く(spec 4.5。照合用の副本)。保管先が結線されていなければ書かない。
// 中身は SITG(用途別の鍵 config で暗号化。AAD の ID は def_hash の SHA-256 の値)で、平文の定義を保管先に残さない。
// 書いた版(既にあればその版)は objects[] に記録する(復元テストの表の 2 が記録した版 ID で読む)。
func (d *Deps) writeConfigCopy(ctx context.Context, hash string, norm []byte) error {
	if d.Object == nil {
		return nil
	}
	key := "config/" + hash
	vs, err := d.Object.ListVersions(ctx, key)
	if err != nil {
		return err
	}
	now, err := d.Clock.Now(ctx)
	if err != nil {
		return err
	}
	for _, v := range vs {
		if v.Key == key && !v.DeleteMarker {
			return state.RecordObject(ctx, d.State, state.ObjectRec{Key: key, VersionID: v.VersionID, Size: v.Size, WrittenAt: now.UTC().Format(time.RFC3339)})
		}
	}
	ck, err := d.KeyS.DerivedKey("config")
	if err != nil {
		return err
	}
	plain, err := json.Marshal(map[string]json.RawMessage{"normalized": norm})
	if err != nil {
		return internalErr()
	}
	sealed, err := d.Codec.Encode(contract.MagicConfig, strings.TrimPrefix(hash, contract.HashPrefix),
		contract.Header{Kind: contract.MagicConfig, FormatVersion: contract.FormatVersion, Mark: configMark(ck)}, plain, ck)
	if err != nil {
		return err
	}
	res, err := d.Object.Put(ctx, key, bytes.NewReader(sealed), contract.RetentionDaily)
	if err != nil {
		return err
	}
	return state.RecordObject(ctx, d.State, state.ObjectFromPut(key, res, now))
}

// configMark は SITG の見出しの印。契約の KeyService がマスター鍵の鍵 ID を返さないので、用途別の鍵の SHA-256 の先頭 4 バイトで代える(Recorder.journalMark と同じ逸脱。報告)。
func configMark(key []byte) []byte {
	h := sha256.Sum256(key)
	return h[:4]
}

// writeDefFile は定義ファイルの内容を正規化した値が提案の定義と違えば書き戻す(spec 3.6)。書けなければ DEF_FILE_WRITE_FAILED。
// 書く内容は正規化した JSON(YAML の部分集合なので def validate がそのまま読める)。
func (d *Deps) writeDefFile(ctx context.Context, norm []byte) error {
	path := d.defFilePath()
	if raw, err := os.ReadFile(path); err == nil {
		if got, _, issues, verr := d.State.ValidateDef(ctx, raw, true); verr == nil && (len(issues) == 0 || string(issues) == "[]" || string(issues) == "null") && bytes.Equal(got, norm) {
			return nil
		}
	}
	if err := writeFileAtomicMode(path, append(append([]byte{}, norm...), '\n'), 0o644); err != nil {
		return newErr(contract.CodeDefFileWriteFailed, nil)
	}
	return nil
}
