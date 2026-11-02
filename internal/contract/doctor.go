package contract

// doctor の検査項目(spec 3.1.2「doctor の検査項目」の表。順序は表の順)。結果の checks[] は段階までの項目と advisory を表の順に並べる。
// 判定の実装は U1g。schema/cli/results.json の doctor の name・severity・stage の列挙と照合する(contract_enums_test.go)。

// DoctorCheck は 1 項目。Stage は検査できる最初の段階(bootstrap < defined < sealed)、All は「すべて」。
type DoctorCheck struct {
	Name     string
	Stage    string // bootstrap / defined / sealed / all
	Severity string // required / advisory
}

var DoctorChecks = []DoctorCheck{
	{"service_user", "bootstrap", "required"},
	{"state_dir_fs", "bootstrap", "required"},
	{"container_runtime", "bootstrap", "required"},
	{"check_images", "bootstrap", "required"},
	{"timer_permission", "bootstrap", "required"},
	{"listening_ports", "bootstrap", "required"},
	{"directory_join", "bootstrap", "required"},
	{"admin_access_closed", "bootstrap", "required"},
	{"storage_reachable", "defined", "required"},
	{"time_skew", "defined", "required"},
	{"timers_registered", "sealed", "required"},
	{"file_modes", "sealed", "required"},
	{"adapter_dir_modes", "bootstrap", "required"},
	{"disk_encryption", "all", "advisory"},
	{"swap_disabled", "all", "advisory"},
}

// DoctorStages は doctor の stage の値(段階の順)。
var DoctorStages = []string{"bootstrap", "defined", "sealed"}

// DoctorCheckImagesCount は check_images の count(対象の件数)。同梱のチェックが無いイテレーション 1 は 0 で、
// 対象 0 件で合格とし、検査を飛ばしたように見せない(spec 3.1.2。D-0124 の監督の追記 3 の (2))。count を持つのは check_images だけ。
const DoctorCheckImagesCount = 0

// DoctorCheckNames は DoctorChecks の名前の列。
func DoctorCheckNames() []string {
	r := make([]string, 0, len(DoctorChecks))
	for _, c := range DoctorChecks {
		r = append(r, c.Name)
	}
	return r
}
