package acquire

// 候補の判定(spec 3.3「候補の規則」)のうち source survey の側。材料は sitate.source-survey/v1。

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type surveyMount struct {
	Path       string `json:"path"`
	FsType     string `json:"fs_type"`
	Bytes      *int64 `json:"bytes"`
	TouchesRaw bool   `json:"touches_raw"`
}

type surveyDir struct {
	Path       string `json:"path"`
	Parent     string `json:"parent"`
	FsType     string `json:"fs_type"`
	Bytes      *int64 `json:"bytes"`
	TouchesRaw bool   `json:"touches_raw"`
}

type surveyCluster struct {
	SocketDir     string   `json:"socket_dir"`
	Port          int      `json:"port"`
	Major         *int     `json:"major"`
	RawPaths      []string `json:"raw_paths"`
	Readable      bool     `json:"readable"`
	NamesReadable bool     `json:"names_readable"`
}

type surveyDB struct {
	Name         string `json:"name"`
	Port         int    `json:"port"`
	Bytes        int64  `json:"bytes"`
	LargeObjects bool   `json:"large_objects"`
}

// srvDoc は sitate.source-survey/v1 の判定に使う部分。
type srvDoc struct {
	Mounts            []surveyMount   `json:"mounts"`
	Dirs              []surveyDir     `json:"dirs"`
	Clusters          []surveyCluster `json:"clusters"`
	Databases         []surveyDB      `json:"databases"`
	DatabasesComplete bool            `json:"databases_complete"`
	PrereqMissing     []string        `json:"prereq_missing"`
}

type outMount struct {
	Path   string `json:"path"`
	FsType string `json:"fs_type"`
	Bytes  *int64 `json:"bytes"`
	Class  string `json:"class"`
}

type outDir struct {
	Path      string `json:"path"`
	Label     string `json:"label"`
	Bytes     *int64 `json:"bytes"`
	FsType    string `json:"fs_type"`
	Supported bool   `json:"supported"`
	DBData    bool   `json:"db_data"`
}

type outDB struct {
	Name         string `json:"name"`
	Port         int    `json:"port"`
	Major        int    `json:"major"`
	Bytes        int64  `json:"bytes"`
	LargeObjects bool   `json:"large_objects"`
}

type outCluster struct {
	Port          int  `json:"port"`
	Major         *int `json:"major"`
	NamesReadable bool `json:"names_readable"`
	Supported     bool `json:"supported"`
}

type surveyJudged struct {
	Dirs              []outDir     `json:"dirs"`
	Mounts            []outMount   `json:"mounts"`
	Databases         []outDB      `json:"databases"`
	Clusters          []outCluster `json:"clusters"`
	SizesComplete     bool         `json:"sizes_complete"`
	DatabasesComplete bool         `json:"databases_complete"`
}

func inList(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// under は p が root と同じかその下か(/ の区切りで比べる)。
func under(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }

// mountCountable は mountinfo のマウント点を数えるか(4.1 の根に置けない場所 (i)(iii) に当たるものは数えない)。
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

// mountClass はマウントの種別。数えないときは ""。判定の順: (i)(iii) → (ii) parent → 生のファイルに触れる db_data → ファイルシステムの種類。
func mountClass(path, fs string, touchesRaw bool) string {
	switch {
	case !mountCountable(path):
		return ""
	case inList(contract.RootForbiddenII, path):
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

// fsTypeOut は出力の fs_type(一覧に無い名前は other)。
func fsTypeOut(fs string) string {
	if inList(contract.FsTypesSupported, fs) || inList(contract.FsTypesUncounted, fs) || inList(contract.FsTypesUnsupported, fs) {
		return fs
	}
	return contract.FsTypeOther
}

func judgeSurvey(in srvDoc) surveyJudged {
	out := surveyJudged{Dirs: []outDir{}, Mounts: []outMount{}, Databases: []outDB{}, Clusters: []outCluster{}, SizesComplete: true, DatabasesComplete: in.DatabasesComplete}
	class := map[string]string{}
	for _, m := range in.Mounts {
		c := mountClass(m.Path, m.FsType, m.TouchesRaw)
		if c == "" {
			continue
		}
		class[m.Path] = c
		out.Mounts = append(out.Mounts, outMount{m.Path, fsTypeOut(m.FsType), m.Bytes, c})
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
		out.Dirs = append(out.Dirs, outDir{d.Path, label, d.Bytes, fsTypeOut(d.FsType), inList(contract.FsTypesSupported, d.FsType), d.TouchesRaw})
		out.SizesComplete = out.SizesComplete && d.Bytes != nil
	}
	major := map[int]int{}
	for _, c := range in.Clusters {
		sup := c.Major != nil && *c.Major >= contract.PGMajorMin
		if c.Major != nil {
			major[c.Port] = *c.Major
		}
		out.Clusters = append(out.Clusters, outCluster{c.Port, c.Major, c.NamesReadable, sup})
		out.DatabasesComplete = out.DatabasesComplete && c.Readable
	}
	for _, d := range in.Databases {
		if mj, ok := major[d.Port]; ok && mj >= contract.PGMajorMin {
			out.Databases = append(out.Databases, outDB{d.Name, d.Port, mj, d.Bytes, d.LargeObjects})
		}
	}
	return out
}

// Judge は contract.Surveyor を満たす。survey は sitate.source-survey/v1(結果の配列と sizes_complete・databases_complete を返す)
// か、sitate.recv-survey/v1(def と s.SourceClusters から source check の uncovered[] の材料を返す)。
func (s *Surveyor) Judge(_ context.Context, survey json.RawMessage, def []byte) (json.RawMessage, error) {
	var head struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(survey, &head); err != nil {
		return nil, errInternal("output")
	}
	switch head.Format {
	case contract.SourceSurveyFormat:
		var d srvDoc
		if err := json.Unmarshal(survey, &d); err != nil {
			return nil, errInternal("output")
		}
		return json.Marshal(judgeSurvey(d))
	case contract.RecvSurveyFormat:
		return judgeRecvJSON(survey, def, s.SourceClusters)
	}
	return nil, errInternal("output")
}
