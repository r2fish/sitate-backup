package approval

// 承認カードの材料(spec 3.1.2.9 の表の `plan` の列。まとめレビュー H1)。カードは核の dry-run の plan からだけ描くので、
// 人が見る宛先・指紋・差分・定義は核が作る。エージェントの出した JSON をカードの材料にしない。

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

var jsonNull = json.RawMessage("null")

// planDetails は種別ごとの表示の材料を plan に足す。Planner(cli)の追加は、この後に重ねる。
func (a *Approval) planDetails(ctx context.Context, kind string, args, plan map[string]json.RawMessage) error {
	switch kind {
	case "seal":
		return a.sealPlan(ctx, args, plan)
	case "def-change", "def-change-unlisted", "checks-weaken":
		return a.defChangePlan(ctx, args, plan)
	case "source-change", "restore-change":
		return a.sectionPlan(ctx, kind, args, plan)
	case "delay-change":
		return a.delayPlan(ctx, args, plan)
	case "storage-init":
		return a.storageInitPlan(ctx, args, plan)
	}
	return nil
}

type hostPin struct {
	Role      string  `json:"role"`
	Host      string  `json:"host"`
	HostKeyFP *string `json:"host_key_fp"`
}

// sealPlan は seal の plan: 確定する定義、固定するホスト鍵、承認鍵の指紋、既定より弱い項目、定義の写しを読んだ世代(init --adopt のときだけ)。
func (a *Approval) sealPlan(ctx context.Context, args, plan map[string]json.RawMessage) error {
	delete(plan, "def_hash_after") // カードの seal の欄に無い。前提の def_hash が同じ内容を縛る
	plan["definition"] = args["definition"]
	var hks []hostPin
	if _, err := a.getItem(ctx, "host_keys", &hks); err != nil {
		return err
	}
	pins := []hostPin{}
	for _, h := range hks {
		if h.Role == "source" || h.Role == "restore_dest" {
			pins = append(pins, h)
		}
	}
	plan["host_keys"] = mustJSON(pins)
	var ak struct {
		FP string `json:"key_fp"`
	}
	if _, err := a.getItem(ctx, "approval_key", &ak); err != nil {
		return err
	}
	if ak.FP != "" {
		plan["approval_key_fp"] = mustJSON(ak.FP)
	}
	plan["weaker_items"] = mustJSON(weakItems(args["definition"]))
	plan["definition_from"] = jsonNull
	return nil
}

// weakItems は安全に効く既定値つきの項目のうち、既定値より弱いものの JSON Pointer。
// 既定値を持つのは approval_delay_hours だけをここで判定する(checks・auto_hold_days の既定との比較は未実装)。
func weakItems(def json.RawMessage) []string {
	out := []string{}
	var d struct {
		Hours *int `json:"approval_delay_hours"`
	}
	if json.Unmarshal(def, &d) == nil && d.Hours != nil && *d.Hours < defaultDelayHours {
		out = append(out, "/approval_delay_hours")
	}
	return out
}

// defChangePlan は def-change 系の plan: 正規化した定義の差分 diff[]、変更後の費用の上限。月額の見積もりは出さず null(費用の部品が無い間)。
func (a *Approval) defChangePlan(ctx context.Context, args, plan map[string]json.RawMessage) error {
	before, _, err := a.State.ReadDef(ctx)
	if err != nil {
		return err
	}
	bm, err := flatten(before)
	if err != nil {
		return internalErr()
	}
	am, err := flatten(args["definition"])
	if err != nil {
		return internalErr()
	}
	paths, err := changedPaths(before, args["definition"])
	if err != nil {
		return internalErr()
	}
	type dEl struct {
		Pointer string          `json:"pointer"`
		Before  json.RawMessage `json:"before"`
		After   json.RawMessage `json:"after"`
	}
	diff := []dEl{}
	for _, p := range paths {
		diff = append(diff, dEl{p, rawOrNull(bm, p), rawOrNull(am, p)})
	}
	sort.SliceStable(diff, func(i, j int) bool { return diff[i].Pointer < diff[j].Pointer })
	plan["diff"] = mustJSON(diff)
	var d struct {
		Ceiling *int64 `json:"cost_ceiling_yen"`
	}
	if json.Unmarshal(args["definition"], &d) == nil && d.Ceiling != nil {
		plan["cost_ceiling_yen"] = mustJSON(*d.Ceiling)
	}
	plan["estimate_after_jpy"], plan["over_ceiling"] = jsonNull, jsonNull
	return nil
}

func rawOrNull(m map[string]string, p string) json.RawMessage {
	if v, ok := m[p]; ok {
		return json.RawMessage(v)
	}
	return jsonNull
}

// sectionPlan は source-change・restore-change の plan: 変更前後の節(admin_user とホスト鍵の指紋は定義に入れないので plan.after に入れない)、
// カードに示すホスト鍵の指紋(提案の節に書かれた値。登録を伴う変更では Surveyor が資格情報なしで取得した値で、書かれた値と違えば提案を作らない)、
// 登録と秘密を要するか、source-change の admin_user(登録を伴う変更で必須)。取得できなければ plan.errors[] に載せる(D-0166)。
func (a *Approval) sectionPlan(ctx context.Context, kind string, args, plan map[string]json.RawMessage) error {
	key := map[string]string{"source-change": "source", "restore-change": "restore"}[kind]
	cur, _, err := a.State.ReadDef(ctx)
	if err != nil {
		return err
	}
	before := sectionOf(cur, key)
	if len(before) == 0 {
		before = jsonNull
	}
	var sec struct {
		FP        *string `json:"host_key_fp"`
		AdminUser string  `json:"admin_user"`
		Host      string  `json:"host"`
		Adapter   string  `json:"adapter"`
		Dest      struct {
			At string `json:"at"`
		} `json:"dest"`
	}
	_ = json.Unmarshal(args[key], &sec)
	after := withoutKeys(args[key], "admin_user", "host_key_fp")
	plan["before"], plan["after"] = before, after
	needs := sectionNeedsSecret(key, before, after)
	plan["needs_secret"] = mustJSON(needs)
	plan["host_key_fp"] = jsonNull
	if sec.FP != nil {
		plan["host_key_fp"] = mustJSON(*sec.FP)
	}
	if key == "source" && needs {
		if sec.AdminUser == "" {
			return argErr("--source", "missing") // 再登録に使う管理用の利用者が無い
		}
		if !adminUserRe.MatchString(sec.AdminUser) {
			return argErr("--source", "format")
		}
	}
	if sec.AdminUser != "" {
		plan["admin_user"] = mustJSON(sec.AdminUser)
	}
	if needs && a.Surveyor != nil {
		return a.fetchHostKeyInto(ctx, key, sec.Host, sec.Adapter, sec.Dest.At, sec.FP, plan)
	}
	return nil
}

var adminUserRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// withoutKeys は節から指定の項目を除く(節が JSON の object でなければそのまま)。
func withoutKeys(sec json.RawMessage, keys ...string) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(sec, &m) != nil || m == nil {
		return sec
	}
	for _, k := range keys {
		delete(m, k)
	}
	b, _ := json.Marshal(m)
	return b
}

// fetchHostKeyInto は登録の相手のホスト鍵を取り、plan.host_key_fp に載せる。ssh-host でない復元先は取らない(作成 / 破棄の組は指紋を持たない)。
func (a *Approval) fetchHostKeyInto(ctx context.Context, key, host, adapter, at string, given *string, plan map[string]json.RawMessage) error {
	target := "source"
	port := 22
	if key == "restore" {
		target = "restore_host"
		if adapter != "" && adapter != "ssh-host" {
			return nil
		}
		u, err := url.Parse(at)
		if err != nil || u.Hostname() == "" {
			return nil
		}
		host = u.Hostname()
		if p, perr := strconv.Atoi(u.Port()); perr == nil {
			port = p
		}
	}
	fp, err := a.Surveyor.FetchHostKey(ctx, host, port)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		reason := "connect"
		var se *contract.SSHError
		if errors.As(err, &se) && se.Reason == "timeout" {
			reason = "timeout"
		}
		plan["errors"] = mustJSON([]map[string]any{{"code": contract.CodeStorageUnreachable, "detail": map[string]any{"target": target, "reason": reason, "http_status": nil}}})
		return nil
	}
	if given != nil && *given != fp {
		if key == "restore" {
			return apiErr(contract.CodeRestoreHostkeyMismatch, map[string]any{"expected_fp": *given, "seen_fp": fp, "session_id": nil})
		}
		return hostkeyErr(*given, fp)
	}
	plan["host_key_fp"] = mustJSON(fp)
	return nil
}

// sectionNeedsSecret は登録(と管理用の秘密)を要する変更か。登録を要さないのは、登録に関わらない項目だけの変更
// (元サーバーの source.declined・source.exclude、復元先の restore.ssh_from・restore.erase_poweroff)で、それ以外の変更・節の新設は要する。
func sectionNeedsSecret(key string, before, after json.RawMessage) bool {
	if string(before) == "null" {
		return true
	}
	skip := map[string][]string{"source": {"declined", "exclude"}, "restore": {"ssh_from", "erase_poweroff"}}[key]
	strip := func(raw json.RawMessage) string {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return string(raw)
		}
		for _, k := range skip {
			delete(m, k)
		}
		b, _ := json.Marshal(m)
		if c, err := contract.CanonicalJSON(b); err == nil { // 入れ子のキーの順を揃えて比べる
			return string(c)
		}
		return string(b)
	}
	return strip(before) != strip(after)
}

// delayPlan は delay-change の plan: 変更前後の遅延時間(定義に書かれていなければ既定)。
func (a *Approval) delayPlan(ctx context.Context, args, plan map[string]json.RawMessage) error {
	cur, _, err := a.State.ReadDef(ctx)
	if err != nil {
		return err
	}
	var d struct {
		Hours *int `json:"approval_delay_hours"`
	}
	_ = json.Unmarshal(cur, &d)
	before := defaultDelayHours
	if d.Hours != nil {
		before = *d.Hours
	}
	plan["before"], plan["after"] = mustJSON(before), args["hours"]
	return nil
}

// storageInitPlan は storage-init の plan: 鍵を置く保管先、参照名、--reissue か。--endpoint・--bucket・--prefix が承認済みの定義の
// storage[0] と違えば提案を作らず ALREADY_INITIALIZED(保管先を替えるときは spec 3.1.2.1 の分岐 6。人が入れた管理用鍵を別の保管先へ渡さない)。
func (a *Approval) storageInitPlan(ctx context.Context, args, plan map[string]json.RawMessage) error {
	cur, _, err := a.State.ReadDef(ctx)
	if err != nil {
		return err
	}
	var d struct {
		Storage []struct {
			Endpoint string `json:"endpoint"`
			Bucket   string `json:"bucket"`
			Prefix   string `json:"prefix"`
			KeyRef   string `json:"key_ref"`
		} `json:"storage"`
	}
	if json.Unmarshal(cur, &d) != nil || len(d.Storage) == 0 { // 承認済みの定義に保管先が無い間(seal の前)は照らす先が無い
		return nil
	}
	s := d.Storage[0]
	for name, want := range map[string]string{"endpoint": s.Endpoint, "bucket": s.Bucket, "prefix": s.Prefix} {
		var got string
		if raw, ok := args[name]; ok && (json.Unmarshal(raw, &got) != nil || got != want) {
			return apiErr(contract.CodeAlreadyInitialized, nil)
		}
	}
	plan["bucket"] = mustJSON(map[string]string{"endpoint": s.Endpoint, "bucket": s.Bucket, "prefix": s.Prefix})
	plan["key_ref"] = mustJSON(s.KeyRef)
	_, reissue := args["reissue"]
	plan["reissue"] = mustJSON(reissue)
	return nil
}

// sshHostArg は source enroll の --host(ssh://<user>@<host>:22。IPv6 は [] で囲む)。
func sshHostArg(user, host string) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "ssh://" + user + "@" + host + ":22"
}
