package contract

import "encoding/json"

// ストリーム sitate.stream/v1 の見出し(spec 3.2.2)。見出しは JSON 1 行と改行。data の本体は見出しの直後から size バイト。
// 見出しのスキーマは schema/adapter/stream.json(#/$defs/frame)。

// ストリームの見出しの type と値。
const (
	FrameTypeFile     = "file"
	FrameTypeRetry    = "file-retry"
	FrameTypeUnchg    = "unchanged"
	FrameTypeDir      = "dir"
	FrameTypeSymlink  = "symlink"
	FrameTypeDump     = "dump"
	FrameTypeData     = "data"
	FrameTypeItemEnd  = "item_end"
	FrameTypeEnd      = "end"
	DumpFormatGlobals = "globals"
	DumpFormatCustom  = "custom"
	ItemOK            = "ok"
	ItemChanged       = "changed"
	ItemError         = "error"
)

// StreamHeader は見出しの全項目の和(type ごとに使う項目が違う)。未知の項目は読めない。
// 値のない項目は省く。ExitStatus は item_end で null を取るため生の JSON で持つ。
type StreamHeader struct {
	Type       string          `json:"type"`
	Path       *string         `json:"path,omitempty"`
	PathB64    *string         `json:"path_b64,omitempty"`
	Target     *string         `json:"target,omitempty"`
	TargetB64  *string         `json:"target_b64,omitempty"`
	Size       *int64          `json:"size,omitempty"`
	Mode       *int            `json:"mode,omitempty"`
	UID        *int64          `json:"uid,omitempty"`
	GID        *int64          `json:"gid,omitempty"`
	MtimeNS    *int64          `json:"mtime_ns,omitempty"`
	Format     string          `json:"format,omitempty"`
	Bytes      *int64          `json:"bytes,omitempty"`
	Hash       string          `json:"hash,omitempty"`
	Status     string          `json:"status,omitempty"`
	ExitStatus json.RawMessage `json:"exit_status,omitempty"`
}

// StreamError はストリームの契約違反。Kind は contract(INTERNAL の component: adapter、reason: output)、
// end_mismatch(end の hash・bytes が核の数えた値と違う。対象を failed)、no_end(end が無い。対象を failed)。
type StreamError struct {
	Kind   string
	Reason string // header | path | order | size | item | hash | bytes | truncated
	Msg    string
}

func (e *StreamError) Error() string { return e.Kind + "/" + e.Reason + ": " + e.Msg }
