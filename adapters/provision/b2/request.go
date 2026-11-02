package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"regexp"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// request は要求(spec 3.2.6 の操作の表)。未知の項目は受けない。
type request struct {
	Contract         string                       `json:"contract"`
	Op               string                       `json:"op"`
	Endpoint         string                       `json:"endpoint"`
	Bucket           string                       `json:"bucket"`
	Prefix           *string                      `json:"prefix"`
	Versioning       *bool                        `json:"versioning"`
	LockMode         string                       `json:"lock_mode"`
	DefaultRetention *int64                       `json:"default_retention_days"`
	Roles            []string                     `json:"roles"`
	MaxRetentionDays *int64                       `json:"max_retention_days"`
	KeyIDs           []string                     `json:"key_ids"`
	KeyRole          *string                      `json:"key_role"`
	Secrets          map[string]map[string]string `json:"secrets"`
}

var (
	endpointRe = regexp.MustCompile(`^https://s3\.[a-z0-9-]+\.backblazeb2\.com$`)
	prefixRe   = regexp.MustCompile(`^[A-Za-z0-9/-]*$`)
	bucketRe   = regexp.MustCompile(`^[A-Za-z0-9-]{1,63}$`)
)

// readRequest は標準入力の 1 行目を読み、形を検査する。版の違いは reason: contract、ほかの不正は reason: request。
func readRequest(in io.Reader) (*request, *provErr) {
	line, err := bufio.NewReaderSize(in, 1<<16).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, internalErr("request")
	}
	var probe struct {
		Contract string `json:"contract"`
	}
	if json.Unmarshal(line, &probe) != nil {
		return nil, internalErr("request")
	}
	if probe.Contract != contract.AdapterContract["provision"] {
		return nil, internalErr("contract")
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var r request
	if dec.Decode(&r) != nil || !r.shapeOK() {
		return nil, internalErr("request")
	}
	if !endpointRe.MatchString(r.Endpoint) { // 上の形でなければ事業者を呼ばない
		return nil, argInvalid("endpoint")
	}
	return &r, nil
}

// shapeOK は op ごとの必須の項目と秘密の枠を検査する。
func (r *request) shapeOK() bool {
	if r.Bucket == "" {
		return false
	}
	_, hasAdmin := r.Secrets["admin"]
	_, hasKey := r.Secrets["key"]
	switch r.Op {
	case "create-bucket":
		return hasAdmin && r.Versioning != nil && r.DefaultRetention != nil && r.LockMode == "COMPLIANCE"
	case "issue-keys":
		return hasAdmin && r.Prefix != nil && len(r.Roles) > 0 && r.validRoles()
	case "revoke-keys":
		return hasAdmin && r.KeyIDs != nil && (len(r.Roles) == 0 || r.validRoles())
	case "check":
		if r.Prefix == nil || hasAdmin == hasKey {
			return false
		}
		return hasAdmin || (r.KeyRole != nil && (*r.KeyRole == "write" || *r.KeyRole == "read"))
	}
	return false
}

func (r *request) validRoles() bool {
	for _, x := range r.Roles {
		if x != "write" && x != "read" {
			return false
		}
	}
	return true
}

// creds は枠の鍵(key_id と secret)を返す。
func (r *request) creds(slot string) (keyID, secret string) {
	m := r.Secrets[slot]
	return m["key_id"], m["secret"]
}
