package main

import (
	"errors"
	"regexp"
	"strings"
)

// provErr は結果 JSON の error(spec 3.2.1 の error の形)。orphaned は error.orphaned_key_ids[]。
type provErr struct {
	code     string
	detail   map[string]any
	orphaned []string
}

func (e *provErr) Error() string { return e.code }

func internalErr(reason string) *provErr {
	return &provErr{code: "INTERNAL", detail: map[string]any{"component": "adapter", "reason": reason}}
}

func argInvalid(flag string) *provErr {
	return &provErr{code: "ARG_INVALID", detail: map[string]any{"flag": flag, "reason": "format"}}
}

func bucketNotOwned(reason string) *provErr {
	return &provErr{code: "BUCKET_NOT_OWNED", detail: map[string]any{"reason": reason}}
}

var (
	capRe     = regexp.MustCompile(`cap_exceeded`)
	suspendRe = regexp.MustCompile(`suspend|disabled|closed|blocked`)
	paymentRe = regexp.MustCompile(`payment|billing`)
)

// mapAPIError は事業者の応答の失敗を原因コードに写す(spec 3.1.3 の保管先の応答の写像)。role は拒否された鍵の枠(admin / write / read)。
func mapAPIError(err error, role string) *provErr {
	var pe *provErr
	if errors.As(err, &pe) {
		return pe
	}
	var ae *apiError
	if !errors.As(err, &ae) {
		return internalErr("output")
	}
	if ae.netReason != "" {
		return unreachable(ae.netReason, nil)
	}
	switch {
	case ae.status == 429:
		return unreachable("throttled", &ae.status)
	case ae.status >= 500:
		return unreachable("server_error", &ae.status)
	}
	text := strings.ToLower(ae.code + " " + ae.message)
	if ae.status == 401 || ae.status == 403 {
		if r := denialReason(text); r != "" {
			return &provErr{code: "STORAGE_DENIED", detail: map[string]any{"reason": r, "http_status": ae.status}}
		}
		if ae.auth && ae.status == 401 || ae.code == "bad_auth_token" || ae.code == "expired_auth_token" {
			return keyMissing(role, "key_rejected")
		}
		if ae.auth {
			return &provErr{code: "STORAGE_DENIED", detail: map[string]any{"reason": "unknown", "http_status": ae.status}}
		}
		return keyMissing(role, "denied")
	}
	return &provErr{code: "adapter:b2:UNEXPECTED_RESPONSE", detail: map[string]any{"http_status": ae.status, "b2_code": ae.code}}
}

func denialReason(text string) string {
	switch {
	case capRe.MatchString(text):
		return "provider_cap"
	case suspendRe.MatchString(text):
		return "account_suspended"
	case paymentRe.MatchString(text):
		return "payment"
	}
	return ""
}

func keyMissing(role, reason string) *provErr {
	return &provErr{code: "KEY_MISSING_PERMISSION", detail: map[string]any{"role": role, "reason": reason, "ops": []string{}}}
}

func unreachable(reason string, status *int) *provErr {
	var st any
	if status != nil {
		st = *status
	}
	return &provErr{code: "STORAGE_UNREACHABLE", detail: map[string]any{"target": "storage", "reason": reason, "http_status": st}}
}
