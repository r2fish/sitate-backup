package acquire

// エントリ(sitate.entries/v1。spec 4.4)の内部表現と、パスの符号化。

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"unicode/utf8"
)

// rng は ranges の要素 [chunk_id, version_id, offset, length]。
type rng struct {
	ID, VID  string
	Off, Len int64
}

func (r rng) MarshalJSON() ([]byte, error) { return json.Marshal([]any{r.ID, r.VID, r.Off, r.Len}) }

func (r *rng) UnmarshalJSON(b []byte) error {
	var a []json.RawMessage
	if err := json.Unmarshal(b, &a); err != nil || len(a) != 4 {
		return errors.New("ranges の要素の形")
	}
	for i, p := range []any{&r.ID, &r.VID, &r.Off, &r.Len} {
		if err := json.Unmarshal(a[i], p); err != nil {
			return err
		}
	}
	return nil
}

// entry は 1 つのエントリ。Path は対象 id を先頭に置いた生のバイト列(<target-id>/…。根は <target-id>)。
type entry struct {
	Ref      string
	Target   string
	Path     []byte
	Type     string
	Mode     *int
	UID, GID *int64
	MtimeNS  *int64
	Size     *int64
	Hash     string
	Flags    []string
	Ranges   *[]rng
	LinkTo   []byte // symlink のリンク先
	HasLink  bool
	Format   string
}

type entryJSON struct {
	Ref     string   `json:"ref"`
	T       string   `json:"t"`
	Path    *string  `json:"path,omitempty"`
	PathB64 *string  `json:"path_b64,omitempty"`
	Type    string   `json:"type"`
	Mode    *int     `json:"mode,omitempty"`
	UID     *int64   `json:"uid,omitempty"`
	GID     *int64   `json:"gid,omitempty"`
	MtimeNS *int64   `json:"mtime_ns,omitempty"`
	Size    *int64   `json:"size,omitempty"`
	Hash    string   `json:"hash,omitempty"`
	Flags   []string `json:"flags,omitempty"`
	Ranges  *[]rng   `json:"ranges,omitempty"`
	Target  *string  `json:"target,omitempty"`
	TargetB *string  `json:"target_b64,omitempty"`
	Format  string   `json:"format,omitempty"`
}

// textOrB64 は UTF-8 として正しければ文字列、そうでなければ base64(どちらか 1 つ)。
func textOrB64(b []byte) (text, b64 *string) {
	if utf8.Valid(b) {
		s := string(b)
		return &s, nil
	}
	s := base64.StdEncoding.EncodeToString(b)
	return nil, &s
}

func fromText(text, b64 *string) ([]byte, error) {
	if text != nil {
		return []byte(*text), nil
	}
	if b64 != nil {
		return base64.StdEncoding.DecodeString(*b64)
	}
	return nil, errors.New("path が無い")
}

func (e entry) MarshalJSON() ([]byte, error) {
	j := entryJSON{Ref: e.Ref, T: e.Target, Type: e.Type, Mode: e.Mode, UID: e.UID, GID: e.GID, MtimeNS: e.MtimeNS, Size: e.Size, Hash: e.Hash,
		Flags: e.Flags, Ranges: e.Ranges, Format: e.Format}
	j.Path, j.PathB64 = textOrB64(e.Path)
	if e.HasLink {
		j.Target, j.TargetB = textOrB64(e.LinkTo)
	}
	return json.Marshal(j)
}

func (e *entry) UnmarshalJSON(b []byte) error {
	var j entryJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	p, err := fromText(j.Path, j.PathB64)
	if err != nil {
		return err
	}
	*e = entry{Ref: j.Ref, Target: j.T, Path: p, Type: j.Type, Mode: j.Mode, UID: j.UID, GID: j.GID, MtimeNS: j.MtimeNS, Size: j.Size, Hash: j.Hash,
		Flags: j.Flags, Ranges: j.Ranges, Format: j.Format}
	if j.Target != nil || j.TargetB != nil {
		e.HasLink = true
		if e.LinkTo, err = fromText(j.Target, j.TargetB); err != nil {
			return err
		}
	}
	return nil
}

// relPath は対象の根からの相対パス(根は ".")。
func (e entry) relPath() []byte {
	p := e.Path
	if i := bytes.IndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return []byte(".")
}

// sortEntries は対象 id、パスのバイト順(根を最初)に並べる。
func sortEntries(l []entry) {
	sort.SliceStable(l, func(i, j int) bool {
		if l[i].Target != l[j].Target {
			return l[i].Target < l[j].Target
		}
		ri, rj := l[i].relPath(), l[j].relPath()
		if (string(ri) == ".") != (string(rj) == ".") {
			return string(ri) == "."
		}
		return bytes.Compare(ri, rj) < 0
	})
}

func i64p(v int64) *int64 { return &v }
func intp(v int) *int     { return &v }
