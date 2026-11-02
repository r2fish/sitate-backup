package acquire

// 候補の判定のうち source check の側(spec 3.3「source check の自己診断」の uncovered[]・actionable[])。
// 材料は受け口の survey(sitate.recv-survey/v1)、状態ファイルの source_clusters、定義の対象と source.declined。

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type recvMount struct {
	Path   string `json:"path"`
	FsType string `json:"fs_type"`
}

type recvCluster struct {
	Port  int       `json:"port"`
	Names *[]string `json:"names"`
}

type recvSurvey struct {
	Mounts   []recvMount   `json:"mounts"`
	Clusters []recvCluster `json:"clusters"`
}

type stateCluster struct {
	SocketDir string   `json:"socket_dir"`
	Port      int      `json:"port"`
	Major     int      `json:"major"`
	RawPaths  []string `json:"raw_paths"`
}

type uncovered struct {
	Kind     string  `json:"kind"`
	Path     *string `json:"path"`
	Port     *int    `json:"port"`
	TargetID *string `json:"target_id"`
}

// checkTargets は定義の対象のうち判定に要る部分。
type checkTarget struct {
	ID         string
	Files      string
	Postgresql string
	Port       *int
}

type checkDef struct {
	Host     string
	Targets  []checkTarget
	Declined struct {
		Mounts   map[string]bool
		Clusters map[int]bool
	}
}

func parseCheckDef(def []byte) (checkDef, error) {
	var d struct {
		Source struct {
			Host    string `json:"host"`
			Targets []struct {
				ID         string `json:"id"`
				Files      string `json:"files"`
				Postgresql string `json:"postgresql"`
				Port       *int   `json:"port"`
			} `json:"targets"`
			Declined []struct {
				Mount             *string `json:"mount"`
				PostgresqlCluster *int    `json:"postgresql_cluster"`
			} `json:"declined"`
		} `json:"source"`
	}
	var r checkDef
	if err := json.Unmarshal(def, &d); err != nil {
		return r, errInternal("output")
	}
	r.Host = d.Source.Host
	for _, t := range d.Source.Targets {
		r.Targets = append(r.Targets, checkTarget{ID: t.ID, Files: t.Files, Postgresql: t.Postgresql, Port: t.Port})
	}
	r.Declined.Mounts, r.Declined.Clusters = map[string]bool{}, map[int]bool{}
	for _, x := range d.Source.Declined {
		if x.Mount != nil {
			r.Declined.Mounts[*x.Mount] = true
		}
		if x.PostgresqlCluster != nil {
			r.Declined.Clusters[*x.PostgresqlCluster] = true
		}
	}
	return r, nil
}

func sp(s string) *string { return &s }
func ip(i int) *int       { return &i }

// judgeCheck は uncovered[] を作る(large_object は selftest の材料から来るためここでは作らない)。
func judgeCheck(rs recvSurvey, sc []stateCluster, def checkDef) []uncovered {
	res := judgeMounts(rs, sc, def)
	res = append(res, judgeClusters(rs, sc, def)...)
	sortUncovered(res)
	return res
}

func judgeMounts(rs recvSurvey, sc []stateCluster, def checkDef) []uncovered {
	res := []uncovered{}
	var raw []string
	for _, c := range sc {
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
	// 根を含む最も長いマウント点がそのマウント。下に対象の根があれば覆われたものとする。
	rootBelow := func(p string) bool {
		for _, t := range def.Targets {
			if t.Files != "" && under(t.Files, p) {
				return true
			}
		}
		return false
	}
	for _, m := range rs.Mounts {
		c := mountClass(m.Path, m.FsType, touches(m.Path))
		if c == "" || def.Declined.Mounts[m.Path] {
			continue
		}
		switch {
		case c == contract.MountClassUnsupported:
			res = append(res, uncovered{Kind: "mount_unsupported", Path: sp(m.Path)})
		case rootBelow(m.Path):
		case c == contract.MountClassCandidate:
			res = append(res, uncovered{Kind: "mount", Path: sp(m.Path)})
		default:
			res = append(res, uncovered{Kind: "mount_parent", Path: sp(m.Path)})
		}
	}
	return res
}

func judgeClusters(rs recvSurvey, sc []stateCluster, def checkDef) []uncovered {
	res := []uncovered{}
	major := map[int]int{}
	for _, c := range sc {
		major[c.Port] = c.Major
	}
	covered := func(name string, port int) bool {
		for _, t := range def.Targets {
			if t.Postgresql == name && (t.Port == nil || *t.Port == port) {
				return true
			}
		}
		return false
	}
	seen := map[int]bool{}
	for _, c := range rs.Clusters {
		if seen[c.Port] || def.Declined.Clusters[c.Port] {
			continue
		}
		seen[c.Port] = true
		mj, known := major[c.Port]
		switch {
		case !known:
			res = append(res, uncovered{Kind: "db_cluster", Port: ip(c.Port)})
		case mj < contract.PGMajorMin:
			res = append(res, uncovered{Kind: "db_unsupported", Port: ip(c.Port)})
		case c.Names == nil:
			res = append(res, uncovered{Kind: "db_cluster", Port: ip(c.Port)})
		default:
			for _, n := range *c.Names {
				if n != "postgres" && !covered(n, c.Port) {
					res = append(res, uncovered{Kind: "db", Path: sp(n), Port: ip(c.Port)})
				}
			}
		}
	}
	return res
}

func sortUncovered(u []uncovered) {
	key := func(x uncovered) string {
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

// actionableOf は足せる候補(mount・mount_parent・db・db_cluster)だけを返す。
func actionableOf(u []uncovered) []uncovered {
	r := []uncovered{}
	for _, x := range u {
		if inList(contract.ActionableKinds, x.Kind) {
			r = append(r, x)
		}
	}
	return r
}

// judgeRecvJSON は Surveyor.Judge の recv-survey の側(uncovered[] と actionable[] を返す)。
func judgeRecvJSON(survey json.RawMessage, def []byte, clusters json.RawMessage) (json.RawMessage, error) {
	var rs recvSurvey
	if err := json.Unmarshal(survey, &rs); err != nil {
		return nil, errInternal("output")
	}
	cd, err := parseCheckDef(def)
	if err != nil {
		return nil, err
	}
	var sc []stateCluster
	if len(clusters) > 0 {
		if err := json.Unmarshal(clusters, &sc); err != nil {
			return nil, errInternal("output")
		}
	}
	u := judgeCheck(rs, sc, cd)
	return json.Marshal(map[string]any{"uncovered": u, "actionable": actionableOf(u)})
}
