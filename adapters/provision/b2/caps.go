package main

import (
	"fmt"
	"slices"
	"strings"
)

// 鍵の製品の印(spec 3.2.6)。名前は sitate-<bucket>-<p>-<role>。<p> は prefix の / を - に替えて末尾の - を落としたもの。
const (
	markPrefix = "sitate-"
	nameMax    = 100
)

// keyName は鍵の名前を作る。prefix が英数字・-・/ 以外を含む、または名前が長すぎるときは ARG_INVALID(prefix)。
func keyName(bucket, prefix, role string) (string, *provErr) {
	if !prefixRe.MatchString(prefix) {
		return "", argInvalid("prefix")
	}
	if !bucketRe.MatchString(bucket) {
		return "", argInvalid("bucket")
	}
	n := fmt.Sprintf("%s%s-%s-%s", markPrefix, bucket, prefixPart(prefix), role)
	if len(n) > nameMax {
		return "", argInvalid("prefix")
	}
	return n, nil
}

func prefixPart(prefix string) string {
	return strings.TrimRight(strings.ReplaceAll(prefix, "/", "-"), "-")
}

// 発行する鍵の capability(spec 3.2.6 の操作の表の「許す」の列)。
var (
	writeCaps = []string{"writeFiles", "writeFileRetentions", "listBuckets", "listFiles", "readFiles", "readFileRetentions", "readBuckets", "readBucketRetentions", "deleteFiles"}
	readCaps  = []string{"listBuckets", "listFiles", "readFiles"}
)

func capsForRole(role string) []string {
	if role == "write" {
		return slices.Clone(writeCaps)
	}
	return slices.Clone(readCaps)
}

// opRules は操作の正規化名と B2 の capability の対応(表の順)。all はすべて要る、any はどれか 1 つで足りる。
var opRules = []struct {
	op  string
	all []string
	any []string
}{
	{op: "put", all: []string{"writeFiles"}},
	{op: "list_versions", all: []string{"listBuckets", "listFiles"}},
	{op: "get_version", all: []string{"readFiles"}},
	{op: "get_retention", all: []string{"readFileRetentions"}},
	{op: "get_bucket_config", all: []string{"listBuckets", "readBuckets", "readBucketRetentions"}},
	{op: "extend_retention", all: []string{"writeFileRetentions"}},
	{op: "delete_version", all: []string{"deleteFiles"}},
	{op: "retention_shorten", all: []string{"writeFileRetentions"}},
	{op: "unversioned_delete", all: []string{"writeFiles"}},
	{op: "bucket_policy", all: []string{"writeBuckets"}},
	{op: "lifecycle", all: []string{"writeBuckets"}},
	{op: "default_retention", all: []string{"writeBucketRetentions"}},
	{op: "governance_bypass", all: []string{"bypassGovernance"}},
	{op: "legal_hold", all: []string{"writeFileLegalHolds"}},
	{op: "bucket_delete", all: []string{"deleteBuckets"}},
	{op: "key_manage", any: []string{"writeKeys", "listKeys", "deleteKeys"}},
}

// opsFromCaps は B2 の capability を操作の正規化名に写す(事業者の権限名は結果に出さない)。
func opsFromCaps(caps []string) []string {
	ops := []string{}
	for _, r := range opRules {
		ok := len(r.all) > 0 || len(r.any) > 0
		for _, c := range r.all {
			ok = ok && slices.Contains(caps, c)
		}
		if len(r.any) > 0 {
			ok = ok && slices.ContainsFunc(r.any, func(c string) bool { return slices.Contains(caps, c) })
		}
		if ok {
			ops = append(ops, r.op)
		}
	}
	return ops
}
