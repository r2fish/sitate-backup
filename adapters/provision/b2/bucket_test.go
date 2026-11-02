package main

import (
	"strings"
	"testing"
)

func createReq(extra map[string]any) map[string]any {
	m := map[string]any{"versioning": true, "lock_mode": "COMPLIANCE", "default_retention_days": 90}
	for k, v := range extra {
		m[k] = v
	}
	return baseReq("create-bucket", m)
}

func TestCreateBucketNew(t *testing.T) {
	f := newFake(t)
	o := f.run(t, createReq(nil))
	o.mustOK(t)
	if o.res["created"] != true || o.res["lock_mode"] != "COMPLIANCE" || o.res["default_retention_days"] != float64(90) || o.res["versioning"] != true {
		t.Errorf("結果: %s", o.stdout)
	}
	body := f.bodies["b2_create_bucket"][0]
	dr := body["defaultRetention"].(map[string]any)
	if body["fileLockEnabled"] != true || body["bucketType"] != "allPrivate" || dr["mode"] != "compliance" || dr["period"].(map[string]any)["duration"] != float64(90) {
		t.Errorf("作成の本文: %v", body)
	}
}

func TestCreateBucketExisting(t *testing.T) {
	cases := []struct {
		name   string
		bucket *bucketInfo
		code   string
		items  string
		upd    int
	}{
		{"同じ設定", lockedBucket("b1", true, "compliance", 90, "days"), "", "", 0},
		{"年の単位は 365 日換算", lockedBucket("b1", true, "compliance", 1, "years"), "BUCKET_MISMATCH", "default_retention_days", 0},
		{"既定保持だけ無い(補完)", lockedBucket("b1", true, "", 0, ""), "", "", 1},
		{"Governance", lockedBucket("b1", true, "governance", 90, "days"), "BUCKET_MISMATCH", "lock_mode", 0},
		{"Object Lock 無効", lockedBucket("b1", false, "", 0, ""), "BUCKET_MISMATCH", "lock_mode", 0},
		{"日数が違う", lockedBucket("b1", true, "compliance", 30, "days"), "BUCKET_MISMATCH", "default_retention_days", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			f.addBucket(c.bucket)
			o := f.run(t, createReq(nil))
			if c.code == "" {
				o.mustOK(t)
				if o.res["created"] != false {
					t.Errorf("created は false: %s", o.stdout)
				}
			} else {
				o.mustErr(t, c.code)
				if !strings.Contains(o.stdout, `"item":"`+c.items+`"`) {
					t.Errorf("mismatches.item=%s のはず: %s", c.items, o.stdout)
				}
			}
			if f.count("b2_update_bucket") != c.upd {
				t.Errorf("b2_update_bucket %d 回のはず: %v", c.upd, f.calls)
			}
		})
	}
}

func TestCreateBucketMismatchActualValues(t *testing.T) {
	f := newFake(t)
	f.addBucket(lockedBucket("b1", true, "governance", 30, "days"))
	o := f.run(t, createReq(nil))
	o.mustErr(t, "BUCKET_MISMATCH")
	mm := o.err["detail"].(map[string]any)["mismatches"].([]any)
	if len(mm) != 2 || mm[0].(map[string]any)["actual"] != "GOVERNANCE" || mm[1].(map[string]any)["actual"] != float64(30) || mm[1].(map[string]any)["expected"] != float64(90) {
		t.Errorf("mismatches: %s", o.stdout)
	}
	f2 := newFake(t)
	b := lockedBucket("b1", true, "compliance", 90, "days")
	b.Lock.Authorized, b.Lock.Value = false, nil
	f2.addBucket(b)
	o = f2.run(t, createReq(nil))
	o.mustErr(t, "BUCKET_MISMATCH")
	if !strings.Contains(o.stdout, `"actual":null`) {
		t.Errorf("読めなければ actual は null: %s", o.stdout)
	}
}

func TestCreateBucketTakenByOtherAccount(t *testing.T) {
	f := newFake(t)
	f.others["b1"] = true
	o := f.run(t, createReq(nil))
	o.mustErr(t, "BUCKET_NOT_OWNED")
	if o.err["detail"].(map[string]any)["reason"] != "taken" {
		t.Errorf("reason: taken: %s", o.stdout)
	}
}

func TestEndpointMismatchStopsBeforeOtherCalls(t *testing.T) {
	f := newFake(t)
	f.s3URL = "https://s3.eu-central-003.backblazeb2.com"
	o := f.run(t, createReq(nil))
	o.mustErr(t, "BUCKET_NOT_OWNED")
	if o.err["detail"].(map[string]any)["reason"] != "absent" || len(f.calls) != 1 {
		t.Errorf("absent で認可の後に何もしない: %s %v", o.stdout, f.calls)
	}
}

func TestEndpointFormatNoCall(t *testing.T) {
	f := newFake(t)
	for _, ep := range []string{"https://s3.example.invalid", "http://s3.us-west-004.backblazeb2.com", "https://s3.us-west-004.backblazeb2.com/x"} {
		o := f.run(t, createReq(map[string]any{"endpoint": ep}))
		o.mustErr(t, "ARG_INVALID")
		d := o.err["detail"].(map[string]any)
		if d["flag"] != "endpoint" || d["reason"] != "format" {
			t.Errorf("%s: %s", ep, o.stdout)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("事業者を呼ばない: %v", f.calls)
	}
}

func TestRequestValidation(t *testing.T) {
	f := fakeWithBucket(t)
	bad := createReq(map[string]any{"contract": "sitate.provision/v0"})
	o := f.run(t, bad)
	o.mustErr(t, "INTERNAL")
	if o.err["detail"].(map[string]any)["reason"] != "contract" {
		t.Errorf("reason: contract: %s", o.stdout)
	}
	o = f.run(t, createReq(map[string]any{"extra": 1}))
	o.mustErr(t, "INTERNAL")
	if o.err["detail"].(map[string]any)["reason"] != "request" {
		t.Errorf("reason: request: %s", o.stdout)
	}
	o = f.run(t, baseReq("check", map[string]any{"prefix": "p/", "key_role": nil}))
	o.mustOK(t) // admin の check は key_role null
	o = f.run(t, baseReq("frob", nil))
	o.mustErr(t, "INTERNAL")
}
