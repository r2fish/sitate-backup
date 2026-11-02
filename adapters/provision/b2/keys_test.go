package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func issueReq(prefix string, roles ...string) map[string]any {
	return baseReq("issue-keys", map[string]any{"prefix": prefix, "roles": roles, "max_retention_days": 97})
}

func fakeWithBucket(t *testing.T) *fakeB2 {
	f := newFake(t)
	f.addBucket(lockedBucket("b1", true, "compliance", 90, "days"))
	return f
}

func TestIssueKeysAllOrNothingSuccess(t *testing.T) {
	f := fakeWithBucket(t)
	o := f.run(t, issueReq("sitate/", "write", "read"))
	o.mustOK(t)
	issued := o.res["issued"].([]any)
	if len(issued) != 2 || len(o.res) != 2 { // contract と issued だけ。保持の上限の可否を返さない
		t.Fatalf("issued だけ: %s", o.stdout)
	}
	if issued[0].(map[string]any)["role"] != "write" || issued[1].(map[string]any)["role"] != "read" {
		t.Errorf("要求の roles の順: %s", o.stdout)
	}
	bodies := f.bodies["b2_create_key"]
	for i, want := range []string{"sitate-b1-sitate-write", "sitate-b1-sitate-read"} {
		b := bodies[i]
		if b["keyName"] != want || b["namePrefix"] != "sitate/" || fmt.Sprint(b["bucketIds"]) != "[bid-b1]" {
			t.Errorf("鍵の要求 %d: %v", i, b)
		}
	}
	wc := toStrings(bodies[0]["capabilities"])
	rc := toStrings(bodies[1]["capabilities"])
	if !slices.Contains(wc, "writeFiles") || !slices.Contains(wc, "deleteFiles") || slices.Contains(wc, "writeBuckets") || slices.Contains(wc, "writeKeys") || slices.Contains(wc, "writeFileLegalHolds") || slices.Contains(wc, "bypassGovernance") {
		t.Errorf("書き込み鍵の権限: %v", wc)
	}
	if slices.Contains(rc, "writeFiles") || slices.Contains(rc, "deleteFiles") || !slices.Contains(rc, "readFiles") {
		t.Errorf("読み取り鍵の権限: %v", rc)
	}
}

func toStrings(v any) []string {
	var s []string
	for _, x := range v.([]any) {
		s = append(s, x.(string))
	}
	return s
}

func TestIssueKeysSecretOnlyInResultJSON(t *testing.T) {
	f := fakeWithBucket(t)
	o := f.run(t, issueReq("sitate/", "write", "read"))
	o.mustOK(t)
	for _, s := range []string{adminSecret, testToken} {
		if strings.Contains(o.stderr, s) || strings.Contains(o.stdout, s) {
			t.Errorf("管理用鍵の秘密かトークンが出力に現れた")
		}
	}
	for _, k := range o.res["issued"].([]any) {
		sec := k.(map[string]any)["secret"].(string)
		if sec == "" || strings.Contains(o.stderr, sec) {
			t.Errorf("発行した鍵の secret は標準出力の結果 JSON だけ: stderr=%q", o.stderr)
		}
	}
}

func TestIssueKeysPartialFailureRevokesIssued(t *testing.T) {
	f := fakeWithBucket(t)
	f.failNext("b2_create_key", fakeFail{}, fakeFail{status: 403, code: "access_denied", message: "denied"})
	o := f.run(t, issueReq("sitate/", "write", "read"))
	o.mustErr(t, "KEY_MISSING_PERMISSION")
	if _, has := o.err["orphaned_key_ids"]; has {
		t.Errorf("失効できたので orphaned は無い: %s", o.stdout)
	}
	if len(f.keys) != 0 || f.count("b2_delete_key") != 1 {
		t.Errorf("発行した write 鍵を失効させる: keys=%d calls=%v", len(f.keys), f.calls)
	}
}

func TestIssueKeysRollbackFailureReportsOrphans(t *testing.T) {
	f := fakeWithBucket(t)
	f.failNext("b2_create_key", fakeFail{}, fakeFail{status: 400, code: "bad_request", message: "boom"})
	f.failNext("b2_delete_key", fakeFail{status: 400, code: "bad_request", message: "cannot delete"})
	o := f.run(t, issueReq("sitate/", "write", "read"))
	o.mustErr(t, "adapter:b2:UNEXPECTED_RESPONSE")
	ids := o.err["orphaned_key_ids"].([]any)
	if len(ids) != 1 || ids[0] != f.keys[0].ID {
		t.Errorf("失効できなかった鍵は orphaned_key_ids: %s", o.stdout)
	}
}

func TestIssueKeysFirstFailureNoOrphans(t *testing.T) {
	f := fakeWithBucket(t)
	f.failNext("b2_create_key", fakeFail{status: 403, code: "cap_exceeded", message: "x"})
	o := f.run(t, issueReq("sitate/", "write", "read"))
	o.mustErr(t, "STORAGE_DENIED")
	if _, has := o.err["orphaned_key_ids"]; has || f.count("b2_delete_key") != 0 {
		t.Errorf("%s", o.stdout)
	}
}

func TestIssueKeysNameRules(t *testing.T) {
	for _, p := range []string{"a.b/", "a_b/", "日本/", "a b/"} {
		f := fakeWithBucket(t)
		o := f.run(t, issueReq(p, "write"))
		o.mustErr(t, "ARG_INVALID")
		d := o.err["detail"].(map[string]any)
		if d["flag"] != "prefix" || d["reason"] != "format" || len(f.calls) != 0 {
			t.Errorf("%q: %s %v", p, o.stdout, f.calls)
		}
	}
	f := fakeWithBucket(t)
	o := f.run(t, issueReq(strings.Repeat("a", 100)+"/", "write"))
	o.mustErr(t, "ARG_INVALID")
	f = fakeWithBucket(t)
	o = f.run(t, issueReq("team/data/", "read"))
	o.mustOK(t)
	if f.bodies["b2_create_key"][0]["keyName"] != "sitate-b1-team-data-read" {
		t.Errorf("/ は - に替え末尾の - を落とす: %v", f.bodies["b2_create_key"][0])
	}
}

func TestIssueKeysBucketAbsent(t *testing.T) {
	f := newFake(t)
	o := f.run(t, issueReq("sitate/", "write"))
	o.mustErr(t, "BUCKET_NOT_OWNED")
}

// --- revoke-keys ---

func issued(t *testing.T, f *fakeB2) (w, r string) {
	o := f.run(t, issueReq("sitate/", "write", "read"))
	o.mustOK(t)
	is := o.res["issued"].([]any)
	return is[0].(map[string]any)["key_id"].(string), is[1].(map[string]any)["key_id"].(string)
}

func revokeReq(ids []string, roles ...string) map[string]any {
	m := map[string]any{"key_ids": ids}
	if len(roles) > 0 {
		m["roles"] = roles
	}
	return baseReq("revoke-keys", m)
}

func TestRevokeKeys(t *testing.T) {
	f := fakeWithBucket(t)
	w, r := issued(t, f)
	o := f.run(t, revokeReq([]string{w, "ghost", r}))
	o.mustOK(t)
	if fmt.Sprint(o.res["revoked"]) != fmt.Sprint([]string{w, r}) || fmt.Sprint(o.res["not_found"]) != "[ghost]" {
		t.Errorf("%s", o.stdout)
	}
	if f.count("b2_delete_key") != 2 || len(f.keys) != 0 {
		t.Errorf("一覧に無い key_id は事業者を呼ばない: %v", f.calls)
	}
	o = f.run(t, revokeReq([]string{w})) // 再失効は成功(一覧に無い)
	o.mustOK(t)
	if fmt.Sprint(o.res["not_found"]) != "["+w+"]" {
		t.Errorf("%s", o.stdout)
	}
}

func TestRevokeKeysRejectsAllWhenAnyIneligible(t *testing.T) {
	f := fakeWithBucket(t)
	w, _ := issued(t, f)
	f.addBucket(lockedBucket("b1-x", true, "compliance", 90, "days"))
	f.addKey("foreign1", "my-own-key", "bid-b1", "sitate/", "readFiles")
	f.addKey("otherbucket", "sitate-b1-sitate-write", "bid-b1-x", "sitate/", "readFiles")
	f.addKey("unscoped", "sitate-b1-sitate-write", "", "<none>", "readFiles")
	f.addKey(adminKeyID, "admin-key", "", "<none>", "listKeys")
	f.addKey("badname", "sitate-b1-zzz-write", "bid-b1", "sitate/", "readFiles")
	for _, bad := range []string{"foreign1", "otherbucket", "unscoped", "badname", adminKeyID} {
		before := len(f.keys)
		o := f.run(t, revokeReq([]string{w, bad}))
		o.mustErr(t, "ARG_INVALID")
		d := o.err["detail"].(map[string]any)
		if d["flag"] != "key_ids" || d["reason"] != "format" || len(f.keys) != before {
			t.Errorf("%s: 1 件も失効させない: %s", bad, o.stdout)
		}
	}
	if f.count("b2_delete_key") != 0 {
		t.Errorf("失効の呼びは無い")
	}
}

func TestRevokeKeysRoles(t *testing.T) {
	f := fakeWithBucket(t)
	w, r := issued(t, f)
	o := f.run(t, revokeReq([]string{w, r}, "read"))
	o.mustErr(t, "ARG_INVALID")
	if len(f.keys) != 2 {
		t.Errorf("役割が違う鍵が混じれば 1 件も失効させない")
	}
	o = f.run(t, revokeReq([]string{r}, "read"))
	o.mustOK(t)
}

func TestRevokeKeysDoesNotExistRace(t *testing.T) {
	f := fakeWithBucket(t)
	w, _ := issued(t, f)
	f.failNext("b2_delete_key", fakeFail{status: 400, code: "bad_request", message: "applicationKeyId does not exist"})
	o := f.run(t, revokeReq([]string{w}))
	o.mustOK(t)
	if fmt.Sprint(o.res["not_found"]) != "["+w+"]" {
		t.Errorf("一覧の後に消えた鍵は not_found: %s", o.stdout)
	}
}

// --- check ---

func TestCheckAdminFiltersProductKeys(t *testing.T) {
	f := fakeWithBucket(t)
	f.page = 2
	w1, r1 := issued(t, f)
	w2, _ := func() (string, string) {
		o := f.run(t, issueReq("sitate/", "write"))
		o.mustOK(t)
		return o.res["issued"].([]any)[0].(map[string]any)["key_id"].(string), ""
	}()
	f.addBucket(lockedBucket("b1-x", true, "compliance", 90, "days"))
	f.addKey("coll", "sitate-b1-x-sitate-write", "bid-b1-x", "x-sitate", "writeFiles") // 名前が衝突しても範囲が違う
	f.addKey("p2", "sitate-b1-other-write", "bid-b1", "other/", "writeFiles")
	f.addKey("human", "my-key", "bid-b1", "sitate/", "writeFiles")
	o := f.run(t, baseReq("check", map[string]any{"prefix": "x-sitate", "key_role": nil}))
	o.mustOK(t)
	if len(o.res["keys"].([]any)) != 0 {
		t.Errorf("バケット b1 の x-sitate の鍵は無い: %s", o.stdout)
	}
	o = f.run(t, baseReq("check", map[string]any{"prefix": "sitate/", "key_role": nil}))
	o.mustOK(t)
	var ids []string
	for _, k := range o.res["keys"].([]any) {
		ids = append(ids, k.(map[string]any)["key_id"].(string))
	}
	if !slices.Equal(ids, []string{w1, r1, w2}) {
		t.Errorf("印の付いた鍵だけ(再発行で同名が並んでも key_id で区別・ページをまたぐ): %v", ids)
	}
	k0 := o.res["keys"].([]any)[0].(map[string]any)
	if k0["role"] != "write" || k0["prefix"] != "sitate/" || !slices.Contains(toStrings(k0["capabilities"]), "put") {
		t.Errorf("%v", k0)
	}
	if o.res["lock_mode"] != "COMPLIANCE" || o.res["default_retention_days"] != float64(90) || o.res["versioning"] != true {
		t.Errorf("設定: %s", o.stdout)
	}
}

func TestCheckAdminListFailureReturnsError(t *testing.T) {
	f := fakeWithBucket(t)
	f.page = 1
	issued(t, f)
	f.failNext("b2_list_keys", fakeFail{}, fakeFail{status: 400, code: "bad_request", message: "x"})
	o := f.run(t, baseReq("check", map[string]any{"prefix": "sitate/", "key_role": nil}))
	if o.code != 1 || o.err == nil || o.res != nil {
		t.Errorf("途中で読めなければ keys を返さず error: %s", o.stdout)
	}
}

func keyReq(role, id, secret string) map[string]any {
	return map[string]any{"contract": "sitate.provision/v1", "op": "check", "endpoint": testEndpoint, "bucket": "b1", "prefix": "sitate/", "key_role": role,
		"secrets": map[string]any{"key": map[string]any{"key_id": id, "secret": secret}}}
}

func TestCheckWithKeyUsesOnlyThatKey(t *testing.T) {
	f := fakeWithBucket(t)
	w, r := issued(t, f)
	f.authN = map[string]int{}
	f.calls = nil
	for role, id := range map[string]string{"write": w, "read": r} {
		o := f.run(t, keyReq(role, id, f.creds[id].secret))
		o.mustOK(t)
		k := o.res["keys"].([]any)[0].(map[string]any)
		caps := toStrings(k["capabilities"])
		if k["role"] != role || k["key_id"] != id || k["prefix"] != "sitate/" || o.res["versioning"] != nil || o.res["lock_mode"] != nil || o.res["default_retention_days"] != nil {
			t.Errorf("%s: %s", role, o.stdout)
		}
		if role == "write" && (!slices.Contains(caps, "delete_version") || !slices.Contains(caps, "extend_retention") || slices.Contains(caps, "bucket_policy") || slices.Contains(caps, "key_manage") || slices.Contains(caps, "legal_hold")) {
			t.Errorf("書き込み鍵の op: %v", caps)
		}
		if role == "read" && (slices.Contains(caps, "put") || slices.Contains(caps, "delete_version") || !slices.Contains(caps, "get_version") || !slices.Contains(caps, "list_versions")) {
			t.Errorf("読み取り鍵の op: %v", caps)
		}
	}
	if f.authN[adminKeyID] != 0 || len(f.calls) != 2 {
		t.Errorf("管理用鍵を使わず、認可だけを呼ぶ: %v %v", f.authN, f.calls)
	}
}

func TestCheckWithKeyRejected(t *testing.T) {
	f := fakeWithBucket(t)
	o := f.run(t, keyReq("write", "nokey", "nosecret"))
	o.mustErr(t, "KEY_MISSING_PERMISSION")
	d := o.err["detail"].(map[string]any)
	if d["role"] != "write" || d["reason"] != "key_rejected" {
		t.Errorf("%s", o.stdout)
	}
}

func TestOpsFromCapsExcludesUnknown(t *testing.T) {
	if got := opsFromCaps([]string{"shareFiles", "listAllBucketNames"}); len(got) != 0 {
		t.Errorf("%v", got)
	}
	got := opsFromCaps([]string{"listKeys"})
	if !slices.Equal(got, []string{"key_manage"}) {
		t.Errorf("%v", got)
	}
}
