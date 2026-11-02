package harness

import (
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 候補の規則(spec 3.3)の試験ベクトル(testdata/survey/rules.json)を照らす基準の読み方。
// 製品の判定(U1c の Surveyor)ではなく、ベクトルが仕様の文と矛盾しないことを確かめるための参照である。

type SurveyMount struct {
	Path       string `json:"path"`
	FsType     string `json:"fs_type"`
	Bytes      *int64 `json:"bytes"`
	TouchesRaw bool   `json:"touches_raw"`
}

type SurveyDir struct {
	Path       string `json:"path"`
	Parent     string `json:"parent"`
	FsType     string `json:"fs_type"`
	Bytes      *int64 `json:"bytes"`
	TouchesRaw bool   `json:"touches_raw"`
}

type SurveyCluster struct {
	SocketDir     string   `json:"socket_dir"`
	Port          int      `json:"port"`
	Major         *int     `json:"major"`
	RawPaths      []string `json:"raw_paths"`
	Readable      bool     `json:"readable"`
	NamesReadable bool     `json:"names_readable"`
}

type SurveyDB struct {
	Name         string `json:"name"`
	Port         int    `json:"port"`
	Bytes        int64  `json:"bytes"`
	LargeObjects bool   `json:"large_objects"`
}

// SourceSurveyIn は sitate.source-survey/v1 の判定に使う部分。
type SourceSurveyIn struct {
	Mounts            []SurveyMount   `json:"mounts"`
	Dirs              []SurveyDir     `json:"dirs"`
	Clusters          []SurveyCluster `json:"clusters"`
	Databases         []SurveyDB      `json:"databases"`
	DatabasesComplete bool            `json:"databases_complete"`
}

// 3.1.2 の source survey の結果の配列の要素(判定の出力)。
type OutMount struct {
	Path   string `json:"path"`
	FsType string `json:"fs_type"`
	Bytes  *int64 `json:"bytes"`
	Class  string `json:"class"`
}

type OutDir struct {
	Path      string `json:"path"`
	Label     string `json:"label"`
	Bytes     *int64 `json:"bytes"`
	FsType    string `json:"fs_type"`
	Supported bool   `json:"supported"`
	DBData    bool   `json:"db_data"`
}

type OutDB struct {
	Name         string `json:"name"`
	Port         int    `json:"port"`
	Major        int    `json:"major"`
	Bytes        int64  `json:"bytes"`
	LargeObjects bool   `json:"large_objects"`
}

type OutCluster struct {
	Port          int  `json:"port"`
	Major         *int `json:"major"`
	NamesReadable bool `json:"names_readable"`
	Supported     bool `json:"supported"`
}

type SurveyOut struct {
	Mounts            []OutMount   `json:"mounts"`
	Dirs              []OutDir     `json:"dirs"`
	Databases         []OutDB      `json:"databases"`
	Clusters          []OutCluster `json:"clusters"`
	SizesComplete     bool         `json:"sizes_complete"`
	DatabasesComplete bool         `json:"databases_complete"`
}

func under(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }

// RootForbidden は根のパスを (i)(ii)(iii) に照らす(spec 4.1)。値は ok / root_forbidden / invalid_value。
func RootForbidden(p string) string {
	if !strings.HasPrefix(p, "/") || strings.Contains(p, "//") || strings.ContainsRune(p, 0) {
		return "invalid_value"
	}
	if p != "/" {
		for _, c := range strings.Split(p[1:], "/") {
			if c == "" || c == "." || c == ".." {
				return "invalid_value"
			}
		}
	}
	if p == contract.RootForbiddenI {
		return "root_forbidden"
	}
	for _, x := range contract.RootForbiddenII {
		if p == x {
			return "root_forbidden"
		}
	}
	for _, x := range contract.RootForbiddenIII {
		if under(p, x) {
			return "root_forbidden"
		}
	}
	return "ok"
}

func inList(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func isII(p string) bool { return inList(contract.RootForbiddenII, p) }

// mountCountable は mountinfo のマウント点を数えるか(4.1 の (i)(iii) に当たるものは数えない)。
func mountCountable(p string) bool {
	if p == contract.RootForbiddenI {
		return false
	}
	for _, x := range contract.RootForbiddenIII {
		if under(p, x) {
			return false
		}
	}
	return true
}

// MountClass はマウントの種別を決める。数えないときは ""(要素を作らない)。判定の順は spec 3.3。
func MountClass(path, fs string, touchesRaw bool) string {
	switch {
	case !mountCountable(path):
		return ""
	case isII(path):
		return contract.MountClassParent
	case touchesRaw:
		return contract.MountClassDBData
	case inList(contract.FsTypesSupported, fs):
		return contract.MountClassCandidate
	case inList(contract.FsTypesUncounted, fs):
		return ""
	}
	return contract.MountClassUnsupported
}

// FsTypeOut は出力の fs_type(一覧に無い名前は other)。
func FsTypeOut(fs string) string {
	if inList(contract.FsTypesSupported, fs) || inList(contract.FsTypesUncounted, fs) || inList(contract.FsTypesUnsupported, fs) {
		return fs
	}
	return contract.FsTypeOther
}

// JudgeSurvey は source survey の候補の判定(材料 → 結果の配列)。
func JudgeSurvey(in SourceSurveyIn) SurveyOut {
	out := SurveyOut{Mounts: []OutMount{}, Dirs: []OutDir{}, Databases: []OutDB{}, Clusters: []OutCluster{}, SizesComplete: true, DatabasesComplete: in.DatabasesComplete}
	class := map[string]string{}
	for _, m := range in.Mounts {
		c := MountClass(m.Path, m.FsType, m.TouchesRaw)
		if c == "" {
			continue
		}
		class[m.Path] = c
		out.Mounts = append(out.Mounts, OutMount{m.Path, FsTypeOut(m.FsType), m.Bytes, c})
		out.SizesComplete = out.SizesComplete && m.Bytes != nil
	}
	for _, d := range in.Dirs {
		label := ""
		switch {
		case inList(contract.SurveyDirParents, d.Parent):
			label = strings.TrimPrefix(d.Parent, "/")
		case class[d.Parent] == contract.MountClassDBData:
			label = contract.DirLabelDBMount
		default:
			continue
		}
		out.Dirs = append(out.Dirs, OutDir{d.Path, label, d.Bytes, FsTypeOut(d.FsType), inList(contract.FsTypesSupported, d.FsType), d.TouchesRaw})
		out.SizesComplete = out.SizesComplete && d.Bytes != nil
	}
	major := map[int]int{}
	for _, c := range in.Clusters {
		sup := c.Major != nil && *c.Major >= contract.PGMajorMin
		if c.Major != nil {
			major[c.Port] = *c.Major
		}
		out.Clusters = append(out.Clusters, OutCluster{c.Port, c.Major, c.NamesReadable, sup})
		out.DatabasesComplete = out.DatabasesComplete && c.Readable
	}
	for _, d := range in.Databases {
		if mj, ok := major[d.Port]; ok && mj >= contract.PGMajorMin {
			out.Databases = append(out.Databases, OutDB{d.Name, d.Port, mj, d.Bytes, d.LargeObjects})
		}
	}
	return out
}
