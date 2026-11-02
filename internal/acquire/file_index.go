package acquire

// 対象ごとの前回の取得の索引 index/files/<id>(spec 4.5)と、取得アダプタへ渡す previous_index(JSON Lines。spec 3.2.2)。
// 記録の形は index/chunks と同じ(SITS 形式、前に 4 バイトの長さ)。更新は対象の取得が終わったときに丸ごと置き換える。

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type fileIndex struct {
	dir   string
	codec contract.Codec
	keys  contract.KeyService
}

func (f fileIndex) rel(tid string) string { return "index/files/" + tid }
func (f fileIndex) path(tid string) string {
	return filepath.Join(f.dir, filepath.FromSlash(f.rel(tid)))
}

func (f fileIndex) header() contract.Header {
	return contract.Header{Kind: contract.MagicState, FormatVersion: contract.FormatVersion, Mark: make([]byte, contract.MasterKeyIDSize)}
}

// load は前回のエントリを読む。ファイルが無ければ (nil, false, nil)(初回の扱い)。書きかけの末尾は捨てる。
func (f fileIndex) load(tid string) ([]entry, bool, error) {
	b, err := os.ReadFile(f.path(tid))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	key, err := f.keys.DerivedKey("state")
	if err != nil {
		return nil, false, err
	}
	var out []entry
	for off := 0; off+4 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[off : off+4]))
		if n <= 0 || off+4+n > len(b) {
			if n > 0 { // 書きかけの末尾
				break
			}
			return nil, false, nil // 途中の破損: 前回の索引として使わず、初回として全部を読み直す(安全側)
		}
		plain, err := f.codec.Decode(contract.MagicState, f.rel(tid), b[off+4:off+4+n], key)
		var e entry
		if err == nil {
			err = json.Unmarshal(plain, &e)
		}
		if err != nil {
			if off+4+n == len(b) {
				break
			}
			return nil, false, nil
		}
		out = append(out, e)
		off += 4 + n
	}
	return out, true, nil
}

// store は対象のエントリを丸ごと置き換える(一時ファイルに書いてから名前を付け替える)。
func (f fileIndex) store(tid string, l []entry) error {
	key, err := f.keys.DerivedKey("state")
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	for _, e := range l {
		if e.Type == "dump" {
			continue
		}
		plain, _ := json.Marshal(e)
		sealed, err := f.codec.Encode(contract.MagicState, f.rel(tid), f.header(), plain, key)
		if err != nil {
			return err
		}
		buf.Write(binary.BigEndian.AppendUint32(nil, uint32(len(sealed))))
		buf.Write(sealed)
	}
	p := f.path(tid)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return atomicWrite(p, buf.Bytes())
}

// atomicWrite は同じディレクトリの一時ファイルに書いて fsync し、名前を付け替えてからディレクトリを fsync する(spec 4.5 の書き方。電源断で空にならない)。
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error { tmp.Close(); _ = os.Remove(name); return err }
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// previousIndex は取得アダプタへ渡す前回の索引(1 行目は見出し、根 "." の行を最初に、残りを path のバイト順)。
func previousIndex(tid string, l []entry) []byte {
	type row struct {
		rel []byte
		e   entry
	}
	var rows []row
	for _, e := range l {
		if e.Type == "dump" || e.Target != tid {
			continue
		}
		rows = append(rows, row{e.relPath(), e})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if (string(rows[i].rel) == ".") != (string(rows[j].rel) == ".") {
			return string(rows[i].rel) == "."
		}
		return bytes.Compare(rows[i].rel, rows[j].rel) < 0
	})
	var b bytes.Buffer
	hd, _ := json.Marshal(map[string]string{"format": "sitate.previdx/v1", "target": tid})
	b.Write(append(hd, '\n'))
	for _, r := range rows {
		m := map[string]any{"type": map[string]string{"file": "file", "dir": "dir", "symlink": "symlink"}[r.e.Type], "size": deref64(r.e.Size),
			"mtime_ns": deref64(r.e.MtimeNS), "mode": derefInt(r.e.Mode), "uid": deref64(r.e.UID), "gid": deref64(r.e.GID)}
		pt, pb := textOrB64(r.rel)
		if pt != nil {
			m["path"] = *pt
		} else {
			m["path_b64"] = *pb
		}
		line, _ := json.Marshal(m)
		b.Write(append(line, '\n'))
	}
	return b.Bytes()
}

func deref64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
