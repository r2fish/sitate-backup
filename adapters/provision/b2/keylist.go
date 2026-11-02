package main

import (
	"context"
	"slices"
	"strings"
)

const keyPageMax = 1000

// listedKey は b2_list_keys の 1 件。
type listedKey struct {
	Name       string   `json:"keyName"`
	ID         string   `json:"applicationKeyId"`
	BucketIDs  []string `json:"bucketIds"`
	Caps       []string `json:"capabilities"`
	NamePrefix *string  `json:"namePrefix"`
}

// listAllKeys は鍵の一覧を最後のページまで読む。途中で読めなければ error(一部の一覧で失効の対象を決めないため)。
func (s *session) listAllKeys(ctx context.Context) ([]listedKey, *provErr) {
	var all []listedKey
	start := ""
	for {
		body := map[string]any{"accountId": s.accountID, "maxKeyCount": keyPageMax}
		if start != "" {
			body["startApplicationKeyId"] = start
		}
		var res struct {
			Keys []listedKey `json:"keys"`
			Next *string     `json:"nextApplicationKeyId"`
		}
		if e := s.call(ctx, "b2_list_keys", body, &res); e != nil {
			return nil, e
		}
		all = append(all, res.Keys...)
		if res.Next == nil || *res.Next == "" {
			return all, nil
		}
		start = *res.Next
	}
}

// productRole は鍵の名前と範囲が、要求の bucket・bucketID から作った製品の印と完全一致するときの役割を返す(一致しなければ "")。
// prefix が非 nil なら要求の prefix とも一致を求め、nil なら鍵自身の範囲(namePrefix)から作る(revoke-keys の要求は prefix を持たない)。
func (k *listedKey) productRole(bucket, bucketID string, prefix *string) string {
	if !slices.Equal(k.BucketIDs, []string{bucketID}) {
		return ""
	}
	have := ""
	if k.NamePrefix != nil {
		have = *k.NamePrefix
	}
	if prefix != nil && have != *prefix {
		return ""
	}
	for _, role := range []string{"write", "read"} {
		if n, perr := keyName(bucket, have, role); perr == nil && n == k.Name {
			return role
		}
	}
	return ""
}

