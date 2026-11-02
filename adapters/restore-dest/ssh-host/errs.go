package main

import "github.com/r2fish/sitate-backup/internal/contract"

// adapterErr は終了 1 の error(spec 3.2.1)。detail は核が補う項目を持たない。
type adapterErr struct {
	code      string
	detail    map[string]any
	seenFP    string
	resources []contract.AdapterResource
	withRes   bool // create / start の失敗: resources[] を(空でも)載せる
}

func (e *adapterErr) Error() string { return e.code }

func (e *adapterErr) body() map[string]any {
	d := e.detail
	if d == nil {
		d = map[string]any{}
	}
	m := map[string]any{"code": e.code, "detail": d}
	if e.seenFP != "" {
		m["seen_fp"] = e.seenFP
	}
	if e.withRes {
		rs := e.resources
		if rs == nil {
			rs = []contract.AdapterResource{}
		}
		m["resources"] = rs
	}
	return m
}

// withResources は失敗までに作った資源を載せる(start)。
func (e *adapterErr) withResources(rs []contract.AdapterResource) *adapterErr {
	e.withRes, e.resources = true, rs
	return e
}

// internalErr は INTERNAL。reason は request(要求の不正)・contract・output。
func internalErr(reason string) *adapterErr {
	return &adapterErr{code: "INTERNAL", detail: map[string]any{"component": "adapter", "reason": reason}}
}

func notImplemented(feature string) *adapterErr {
	return &adapterErr{code: "NOT_IMPLEMENTED", detail: map[string]any{"feature": feature}}
}

func codeErr(code string) *adapterErr { return &adapterErr{code: code} }

// customErr はアダプタ固有の失敗 adapter:ssh-host:<CODE>(ADAPTER_ERROR に写る)。
func customErr(code string) *adapterErr { return codeErr("adapter:ssh-host:" + code) }

func unreachableErr(reason string) *adapterErr {
	return &adapterErr{code: "unreachable", detail: map[string]any{"reason": reason}}
}

func hostkeyErr(seen string) *adapterErr {
	return &adapterErr{code: "hostkey_mismatch", detail: map[string]any{}, seenFP: seen}
}

func mismatchErr(items []string) *adapterErr {
	return &adapterErr{code: "enroll_mismatch", detail: map[string]any{"mismatches": items}}
}

func diskShortErr(needed, free int64) *adapterErr {
	return &adapterErr{code: "disk_short", detail: map[string]any{"needed_gb": needed, "free_gb": free}}
}

type prereq struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
}

func prereqErr(ps []prereq) *adapterErr {
	if ps == nil {
		ps = []prereq{}
	}
	return &adapterErr{code: "prereq_missing", detail: map[string]any{"prereq": ps}}
}
