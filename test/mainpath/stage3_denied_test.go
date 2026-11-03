//go:build mainpath

package mainpath

// 第 3 段: seal の後の拒否(乗っ取られたエージェントの視点)。gate 経由で承認を要する操作が APPROVAL_REQUIRED になること、
// sitate ユーザーの書き込み鍵でロック中の版を消す・保持を短縮する試みが保管先に拒否されること。

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/store"
)

func (w *s2) denyState() string {
	return w.bak.root(`cd ` + stateDir + ` && (find secrets def -type f 2>/dev/null | sort | xargs -r sha256sum) | sha256sum`)
}

func (w *s2) afterSealDenied(gens []genRec) {
	t := w.t
	before := w.denyState()
	url := accEnv(t, "SITATE_ACCEPT_HOOK_URL") + "/hooks/" + hookDest
	nb, _ := json.Marshal(map[string]string{hookDest: url + "-other"})
	cases := []struct {
		label, stdin string
		argv         []string
	}{
		{"restore-dest enroll", w.sourceAdmin(), []string{"restore-dest", "enroll", "--dest", w.destAt(), "--adapter", "ssh-host", "--admin-ref", "admin", "--secrets-stdin"}},
		{"source enroll", w.sourceAdmin(), []string{"source", "enroll", "--host", w.sourceHostArg(), "--admin-ref", "admin", "--secrets-stdin"}},
		{"init-storage", storageAdmin(), []string{"init-storage", "--endpoint", w.s3.endpoint, "--bucket", w.s3.bucket, "--prefix", "sitate/", "--adapter", provisionAdapter, "--admin-ref", "admin", "--secrets-stdin"}},
		{"notify set", string(nb) + "\n", []string{"notify", "set", "--to", hookDest, "--secret-ref", hookDest, "--secrets-stdin"}},
	}
	for _, c := range cases {
		o := w.g.call(c.stdin, c.argv...)
		if o.Env.OK || o.code() != "APPROVAL_REQUIRED" {
			t.Errorf("seal の後の %s: 期待は APPROVAL_REQUIRED、実際は ok=%v %s(終了 %d)", c.label, o.Env.OK, o.code(), o.Code)
			continue
		}
		t.Logf("seal の後の %s: APPROVAL_REQUIRED(終了 %d)detail=%.200s", c.label, o.Code, o.Env.Error.Detail)
	}
	if after := w.denyState(); after != before {
		t.Errorf("拒否された操作の後で secrets/ または def/ が変わった")
	}
	w.restorePlantDenied()
	w.deleteLockedVersions(gens[len(gens)-1].ref)
}

// deleteLockedVersions は sitate ユーザーの書き込み鍵(バックアップサーバーの秘密ファイルにある鍵と同じ)で、最新の世代のマニフェストとチャンクの
// ロック中の版を消す・保持を短縮する・版指定なしで消す試みを行い、すべて保管先が拒否し、版が読めたままであることを確かめる。
func (w *s2) deleteLockedVersions(ref string) {
	t := w.t
	if got := w.bak.root(`grep -rlF ` + shq(w.s3.wk) + ` ` + stateDir + `/secrets | wc -l`); strings.TrimSpace(got) == "0" {
		t.Fatalf("バックアップサーバーの秘密ファイルに、台本が使う書き込み鍵が無い")
	}
	wr := &store.ObjectStore{Endpoint: w.s3.endpoint, Bucket: w.s3.bucket, AccessKey: w.s3.wk, SecretKey: w.s3.ws, Prefix: "sitate/", Params: oneDay{}}
	rd := &store.ObjectStore{Endpoint: w.s3.endpoint, Bucket: w.s3.bucket, AccessKey: w.s3.rk, SecretKey: w.s3.rs, Prefix: "sitate/", Params: oneDay{}}
	ctx := context.Background()
	vs, err := wr.ListVersions(ctx, "")
	if err != nil {
		t.Fatalf("版の一覧: %v", err)
	}
	var targets []string // key\tversion
	chunks := 0
	for _, v := range vs {
		if v.DeleteMarker {
			continue
		}
		if v.Key == "manifests/"+ref || (strings.HasPrefix(v.Key, "chunks/") && chunks < 3) {
			targets = append(targets, v.Key+"\t"+v.VersionID)
			if strings.HasPrefix(v.Key, "chunks/") {
				chunks++
			}
		}
	}
	if len(targets) < 2 {
		t.Fatalf("消す試みの対象(マニフェストとチャンク)を一覧から選べない: %d 件", len(targets))
	}
	for _, tg := range targets {
		kv := strings.SplitN(tg, "\t", 2)
		key, ver := kv[0], kv[1]
		until, err := wr.RetainUntil(ctx, key, ver)
		if err != nil || !until.After(time.Now().Add(24*time.Hour)) {
			t.Errorf("%s の保持期限が先でない: %v %v", key, until, err)
		}
		if err := wr.DeleteVersion(ctx, key, ver); err == nil {
			t.Errorf("書き込み鍵で %s のロック中の版を消せてしまった", key)
		} else {
			t.Logf("DeleteVersion(%s): 拒否された: %v", key, err)
		}
		if err := wr.Extend(ctx, key, ver, time.Now().Add(time.Hour)); err == nil {
			t.Errorf("書き込み鍵で %s の保持期限を短縮できてしまった", key)
		} else {
			t.Logf("保持の短縮(%s): 拒否された: %v", key, err)
		}
		rc, err := rd.Get(ctx, key, ver)
		if err != nil {
			t.Errorf("試みの後で %s の版が読めない: %v", key, err)
			continue
		}
		rc.Close()
	}
	t.Logf("書き込み鍵でロック中の %d 個の版(マニフェスト 1・チャンク %d)を消す・短縮する試みはすべて拒否され、版は読めたまま", len(targets), chunks)
}
