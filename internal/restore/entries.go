package restore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// rangeRef は エントリの ranges[] の 1 要素 [chunk_id, version_id, offset, length]。
type rangeRef struct {
	ID        string
	VersionID string
	Offset    int64
	Length    int64
}

func (r *rangeRef) UnmarshalJSON(b []byte) error {
	var a []json.RawMessage
	if err := json.Unmarshal(b, &a); err != nil || len(a) != 4 {
		return fmt.Errorf("range")
	}
	for i, p := range []any{&r.ID, &r.VersionID, &r.Offset, &r.Length} {
		if err := json.Unmarshal(a[i], p); err != nil {
			return err
		}
	}
	return nil
}

// entry はマニフェストのエントリ(sitate.entries/v1 の 1 行。spec 4.4)。
type entry struct {
	Ref       string     `json:"ref"`
	T         string     `json:"t"`
	Path      string     `json:"path"`
	PathB64   string     `json:"path_b64"`
	Type      string     `json:"type"`
	Mode      uint32     `json:"mode"`
	UID       int        `json:"uid"`
	GID       int        `json:"gid"`
	MtimeNS   int64      `json:"mtime_ns"`
	Size      int64      `json:"size"`
	Hash      string     `json:"hash"`
	Target    string     `json:"target"`
	TargetB64 string     `json:"target_b64"`
	Format    string     `json:"format"`
	Ranges    []rangeRef `json:"ranges"`
}

// pathBytes はエントリのパスのバイト列(path か、path_b64 を復号したもの)。どちらか 1 つだけを持たなければ ok は偽。
func (e entry) pathBytes() ([]byte, bool) {
	if (e.Path == "") == (e.PathB64 == "") {
		return nil, false
	}
	if e.PathB64 != "" {
		b, err := base64.StdEncoding.DecodeString(e.PathB64)
		return b, err == nil
	}
	return []byte(e.Path), true
}

// pathBytesTarget は symlink の target(target か、target_b64 を復号したもの)。NUL を含む・復号できない・空なら ok は偽。
func (e entry) pathBytesTarget() ([]byte, bool) {
	if e.TargetB64 != "" {
		b, err := base64.StdEncoding.DecodeString(e.TargetB64)
		return b, err == nil && len(b) > 0 && !bytes.Contains(b, []byte{0})
	}
	return []byte(e.Target), e.Target != "" && !strings.Contains(e.Target, "\x00")
}

func (e entry) targetBytes() []byte {
	if e.TargetB64 != "" {
		b, _ := base64.StdEncoding.DecodeString(e.TargetB64)
		return b
	}
	return []byte(e.Target)
}

// loadEntries はマニフェストのエントリ(JSON Lines のチャンク列)を読む。
func (o *objReader) loadEntries(ctx context.Context, m *manifestDoc) ([]entry, error) {
	b, err := o.joined(ctx, m.Entries.Chunks, "entries")
	if err != nil {
		return nil, err
	}
	var out []entry
	for _, ln := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		var e entry
		if json.Unmarshal(ln, &e) != nil {
			return nil, decryptErr("entries", "", "", "", fmtErr())
		}
		out = append(out, e)
	}
	return out, nil
}
