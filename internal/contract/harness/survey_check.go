package harness

import (
	"fmt"
	"sort"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type RecvMount struct {
	Path   string `json:"path"`
	FsType string `json:"fs_type"`
}

type RecvCluster struct {
	Port  int       `json:"port"`
	Names *[]string `json:"names"`
}

type StateCluster struct {
	SocketDir string   `json:"socket_dir"`
	Port      int      `json:"port"`
	Major     int      `json:"major"`
	RawPaths  []string `json:"raw_paths"`
}

type TargetDB struct {
	Name string `json:"name"`
	Port *int   `json:"port"`
}

type Declined struct {
	Mount             *string `json:"mount,omitempty"`
	PostgresqlCluster *int    `json:"postgresql_cluster,omitempty"`
}

// CheckIn は source check の候補の判定の材料(受け口の survey、状態ファイルの source_clusters、定義の対象と断り)。
type CheckIn struct {
	Mounts         []RecvMount    `json:"mounts"`
	Clusters       []RecvCluster  `json:"clusters"`
	SourceClusters []StateCluster `json:"source_clusters"`
	TargetRoots    []string       `json:"target_roots"`
	TargetDBs      []TargetDB     `json:"target_dbs"`
	Declined       []Declined     `json:"declined"`
}

type Uncovered struct {
	Kind string  `json:"kind"`
	Path *string `json:"path"`
	Port *int    `json:"port"`
}

func sp(s string) *string { return &s }
func ip(i int) *int       { return &i }

// JudgeCheck は uncovered[] を作る(large_object は selftest の材料から来るためここでは作らない)。
// 返す順は kind・path・port の昇順(製品の返す順は問わない。試験は並べ替えて比べる)。
func JudgeCheck(in CheckIn) []Uncovered {
	res := judgeMounts(in)
	res = append(res, judgeClusters(in)...)
	SortUncovered(res)
	return res
}

func declinedSets(in CheckIn) (map[string]bool, map[int]bool) {
	m, p := map[string]bool{}, map[int]bool{}
	for _, d := range in.Declined {
		if d.Mount != nil {
			m[*d.Mount] = true
		}
		if d.PostgresqlCluster != nil {
			p[*d.PostgresqlCluster] = true
		}
	}
	return m, p
}

func judgeMounts(in CheckIn) []Uncovered {
	res := []Uncovered{}
	declMount, _ := declinedSets(in)
	var raw []string
	for _, c := range in.SourceClusters {
		raw = append(raw, c.RawPaths...)
	}
	touches := func(p string) bool {
		for _, r := range raw {
			if under(p, r) || under(r, p) {
				return true
			}
		}
		return false
	}
	rootBelow := func(p string) bool {
		for _, r := range in.TargetRoots {
			if under(r, p) {
				return true
			}
		}
		return false
	}
	for _, m := range in.Mounts {
		c := MountClass(m.Path, m.FsType, touches(m.Path))
		if c == "" || declMount[m.Path] {
			continue
		}
		switch {
		case c == contract.MountClassUnsupported:
			res = append(res, Uncovered{"mount_unsupported", sp(m.Path), nil})
		case rootBelow(m.Path):
		case c == contract.MountClassCandidate:
			res = append(res, Uncovered{"mount", sp(m.Path), nil})
		default:
			res = append(res, Uncovered{"mount_parent", sp(m.Path), nil})
		}
	}
	return res
}

func judgeClusters(in CheckIn) []Uncovered {
	res := []Uncovered{}
	_, declPort := declinedSets(in)
	major := map[int]int{}
	for _, c := range in.SourceClusters {
		major[c.Port] = c.Major
	}
	covered := func(name string, port int) bool {
		for _, t := range in.TargetDBs {
			if t.Name == name && (t.Port == nil || *t.Port == port) {
				return true
			}
		}
		return false
	}
	seen := map[int]bool{}
	for _, c := range in.Clusters {
		if seen[c.Port] || declPort[c.Port] {
			continue
		}
		seen[c.Port] = true
		mj, known := major[c.Port]
		switch {
		case !known:
			res = append(res, Uncovered{"db_cluster", nil, ip(c.Port)})
		case mj < contract.PGMajorMin:
			res = append(res, Uncovered{"db_unsupported", nil, ip(c.Port)})
		case c.Names == nil:
			res = append(res, Uncovered{"db_cluster", nil, ip(c.Port)})
		default:
			for _, n := range *c.Names {
				if n != "postgres" && !covered(n, c.Port) {
					res = append(res, Uncovered{"db", sp(n), ip(c.Port)})
				}
			}
		}
	}
	return res
}

// SortUncovered は kind・path・port の昇順(path の null は先頭)に並べる。
func SortUncovered(u []Uncovered) {
	key := func(x Uncovered) string {
		p, port := "", 0
		if x.Path != nil {
			p = *x.Path
		}
		if x.Port != nil {
			port = *x.Port
		}
		return fmt.Sprintf("%s\x00%s\x00%06d", x.Kind, p, port)
	}
	sort.Slice(u, func(i, j int) bool { return key(u[i]) < key(u[j]) })
}
