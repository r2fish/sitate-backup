package cli

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// 起動の試験(完了条件 24): DefaultDeps が組む Deps と各部品の公開フィールドが埋まっていること。
// 漏れが黙って通らないよう、空のままでよいフィールドは理由つきの許可リストだけに置く。
var wireMayBeZero = map[string]string{
	"Deps.Prices":          "価格表の取り込みは完了条件 24 の外(U5)。estimate は NOT_IMPLEMENTED",
	"Deps.DefFile":         "空なら DefFileDefault",
	"Runner.Env":           "SITATE_RECV は Wrap が決める",
	"Keys.HostKeyPubPath":  "空なら既定の場所",
	"Timers.Schedule":      "空なら承認済みの定義、無ければ 02:30",
	"Timers.TZ":            "空なら承認済みの定義、無ければ Asia/Tokyo",
	"Timers.RuntimeDir":    "空なら /run/user/<uid>",
	"Recorder.RunID":       "空なら発行の時刻から作る",
	"Recorder.Recipients":  "空なら承認済みの定義の notify.to",
	"Notifier.Backoff":     "空なら既定",
	"Notifier.Timeout":     "空なら既定",
	"Notifier.RetryMax":    "空なら既定",
	"TestNotifier.RunID":   "空なら時刻から作る",
	"Index.Now":            "空なら実時刻",
	"Retention.Offset":     "製品ビルドの時計は Offsetter でない",
	"Acquire.From":         "空なら source enroll が元サーバーの SSH_CONNECTION から観測する(観測できなければ SOURCE_SETUP_FAILED)",
	"Container.Podman":     "空なら podman",
	"Container.Home":       "空なら核の環境の値",
	"Container.RuntimeDir": "空なら核の環境の値",
	"Container.ExtraEnv":   "試験の切り替え用",
	"Container.User":       "空なら 65534:65534",
	"SourceEnroller.From":  "Acquire.From と同じ",
	"RunCleanup.Podman":    "空なら podman",
}

func TestDefaultDepsFillsEveryField(t *testing.T) {
	d := DefaultDeps(t.TempDir())
	checkFields(t, "Deps", reflect.ValueOf(d).Elem())
	for _, part := range []any{d.Keys, d.Restore, d.Approval, d.Acquire, d.Store, d.Recorder, d.Notifier, d.TestNotifier, d.Adapters, d.KeyS, d.Index, d.Timers} {
		v := reflect.ValueOf(part)
		if v.Kind() != reflect.Ptr || v.IsNil() {
			t.Fatalf("部品が組まれていない: %T", part)
		}
		checkFields(t, v.Elem().Type().Name(), v.Elem())
	}
	if len(d.RunCleanup) < 2 {
		t.Fatalf("RunCleanup: %d", len(d.RunCleanup))
	}
	if c := reflect.ValueOf(d.Acquire).Elem().FieldByName("Container"); c.IsNil() {
		t.Fatal("Acquire.Container")
	} else {
		checkFields(t, "Container", c.Elem())
	}
	if recvHelper == nil {
		t.Fatal("recvHelper")
	}
}

func checkFields(t *testing.T, name string, v reflect.Value) {
	t.Helper()
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if !f.IsExported() {
			continue
		}
		if v.Field(i).IsZero() {
			if _, ok := wireMayBeZero[name+"."+f.Name]; !ok {
				t.Errorf("%s.%s が空(結線の漏れ。意図して空なら wireMayBeZero に理由つきで足す)", name, f.Name)
			}
		}
	}
}

// M7: 製品の結線では StateFiles.Refs が秘密ファイルの有無を引き、秘密が無い key_ref は unresolved_ref になる。
func TestDefaultDepsResolvesSecretRefs(t *testing.T) {
	d := DefaultDeps(t.TempDir())
	ctx := context.Background()
	_, _, issues, err := d.State.ValidateDef(ctx, []byte(applyDef), false)
	if err != nil || !strings.Contains(string(issues), "unresolved_ref") {
		t.Fatalf("秘密が無いのに解決された: %s %v", issues, err)
	}
	for _, ref := range []string{"storage", "restore-init"} {
		_ = d.Secrets.Put(ctx, ref, json.RawMessage(`{}`))
	}
	_, _, issues, err = d.State.ValidateDef(ctx, []byte(applyDef), false)
	if err != nil || strings.Contains(string(issues), "unresolved_ref") {
		t.Errorf("秘密があるのに未解決: %s %v", issues, err)
	}
}
