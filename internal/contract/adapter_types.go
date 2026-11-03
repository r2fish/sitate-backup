package contract

import "encoding/json"

// アダプタ契約の要求・結果・error の型(spec 3.2。スキーマは schema/adapter/)。
// 試験(contract_adapter_test.go)がスキーマと同じ項目を持つことを確かめる。

// AdapterContract は kind ごとの contract 文字列(v1 は完全一致だけを受ける)。
var AdapterContract = map[string]string{
	"acquire": "sitate.acquire/v1", "check": "sitate.check/v1", "restore-dest": "sitate.restore-dest/v1",
	"notify": "sitate.notify/v1", "provision": "sitate.provision/v1",
}

// AdapterSecretSlots は kind ごとの secrets[] に宣言できる枠の名前(spec 3.2.1)。
var AdapterSecretSlots = map[string][]string{
	"acquire": {}, "check": {}, "restore-dest": {"host_key", "restore-dest-ssh", "dest_key"},
	"notify": {"url", "smtp"}, "provision": {"admin", "key"},
}

// AdapterError は終了 1 のときの結果の error(spec 3.2.1)。Detail は核が補う項目を持たない。
type AdapterError struct {
	Code           string            `json:"code"`
	Detail         json.RawMessage   `json:"detail"`
	Resources      []AdapterResource `json:"resources,omitempty"`
	SeenFP         string            `json:"seen_fp,omitempty"`
	OrphanedKeyIDs []string          `json:"orphaned_key_ids,omitempty"`
}

// AdapterErrorResult は終了 1 の標準出力の 1 行。
type AdapterErrorResult struct {
	Contract string       `json:"contract"`
	Error    AdapterError `json:"error"`
}

// AcquireTarget は取得アダプタの要求の対象。
type AcquireTarget struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Spec struct {
		Path string `json:"path"`
		Port int    `json:"port,omitempty"`
	} `json:"spec"`
}

// AcquireRequest は取得アダプタの要求。PreviousIndex は無ければ nil(JSON の null)。
type AcquireRequest struct {
	Contract      string        `json:"contract"`
	Target        AcquireTarget `json:"target"`
	PreviousIndex *string       `json:"previous_index"`
}

// AcquireItem は結果の errors[]・warnings[] の要素。パス(path_b64)か item_ref のどちらか 1 つ。
type AcquireItem struct {
	Code    string  `json:"code"`
	PathB64 *string `json:"path_b64,omitempty"`
	ItemRef *string `json:"item_ref,omitempty"`
}

// AcquireStats は取得の結果の stats。
type AcquireStats struct {
	Files             int64 `json:"files"`
	Bytes             int64 `json:"bytes"`
	Errors            int64 `json:"errors"`
	ErrorsOmitted     int64 `json:"errors_omitted"`
	WarningsOmitted   int64 `json:"warnings_omitted"`
	ChangedDuringRead int64 `json:"changed_during_read"`
}

// AcquireResult は取得アダプタの結果(end のフレームの直後の 1 行)。
type AcquireResult struct {
	Contract string        `json:"contract"`
	Status   string        `json:"status"`
	Stats    AcquireStats  `json:"stats"`
	Errors   []AcquireItem `json:"errors"`
	Warnings []AcquireItem `json:"warnings"`
}

// CheckInput はチェックの入力(kind: stream は FD、tree は Path、db は Dumps)。
type CheckInput struct {
	Kind  string      `json:"kind"`
	FD    int         `json:"fd,omitempty"`
	Path  string      `json:"path,omitempty"`
	Dumps []CheckDump `json:"dumps,omitempty"`
}

// CheckDump は db 入力のダンプ 1 つ。
type CheckDump struct {
	Format string `json:"format"`
	Path   string `json:"path"`
}

// CheckBaseline は取得時チェックの基準(spec 3.2.3)。
type CheckBaseline struct {
	SnapshotRef string             `json:"snapshot_ref"`
	Files       int64              `json:"files"`
	Bytes       int64              `json:"bytes"`
	ExtHist     map[string]float64 `json:"ext_hist"`
}

// CheckRequest はチェックの要求。Baseline は無ければ nil。
type CheckRequest struct {
	Contract string             `json:"contract"`
	Phase    string             `json:"phase"`
	TargetID string             `json:"target_id"`
	Input    CheckInput         `json:"input"`
	Baseline *CheckBaseline     `json:"baseline"`
	Params   map[string]float64 `json:"params"`
}

// CheckFinding はチェックの所見。item_ref・path・path_b64 のどれか 1 つ。
type CheckFinding struct {
	Code    string `json:"code"`
	ItemRef string `json:"item_ref,omitempty"`
	Path    string `json:"path,omitempty"`
	PathB64 string `json:"path_b64,omitempty"`
}

// CheckResult はチェックの結果。result は pass / warn / fail だけ(error は核が置く)。
type CheckResult struct {
	Contract      string             `json:"contract"`
	Name          string             `json:"name"`
	Version       string             `json:"version"`
	Result        string             `json:"result"`
	Metrics       map[string]float64 `json:"metrics"`
	Findings      []CheckFinding     `json:"findings"`
	FindingsTotal int64              `json:"findings_total"`
}

// RestoreDest は要求の dest(定義の restore.dest)。
type RestoreDest struct {
	At     string `json:"at"`
	KeyRef string `json:"key_ref"`
}

// RestoreDestRequest は復元先アダプタの要求。Op で使う項目が決まる(スキーマの oneOf)。
type RestoreDestRequest struct {
	Contract      string            `json:"contract"`
	Op            string            `json:"op"`
	SessionID     *string           `json:"session_id,omitempty"`
	Dest          RestoreDest       `json:"dest"`
	DryRun        bool              `json:"dry_run,omitempty"` // enroll だけ。真なら接続してホスト鍵と前提を読むだけで何も置かない(D-0165)
	Needs         *RestoreNeeds     `json:"needs,omitempty"`
	HostKeyFP     *string           `json:"host_key_fp,omitempty"`
	AuthorizedKey string            `json:"authorized_key,omitempty"`
	AllowSSHFrom  []string          `json:"allow_ssh_from,omitempty"`
	EgressAllow   []string          `json:"egress_allow,omitempty"`
	EgressHost    string            `json:"egress_host,omitempty"`
	Resources     []AdapterResource `json:"resources,omitempty"`
	Secrets       map[string]string `json:"secrets"`
}

// RestoreNeeds は create / start の needs。
type RestoreNeeds struct {
	DiskGB    int64  `json:"disk_gb"`
	SizeClass string `json:"size_class,omitempty"`
}

// NotifyRequest は通知アダプタの要求。Event は 4.8 のイベント。
type NotifyRequest struct {
	Contract string            `json:"contract"`
	To       string            `json:"to"`
	Event    json.RawMessage   `json:"event"`
	Secrets  map[string]string `json:"secrets"`
}

// NotifyResult は通知アダプタの結果。
type NotifyResult struct {
	Contract    string  `json:"contract"`
	Delivered   bool    `json:"delivered"`
	Channel     string  `json:"channel"`
	Reason      *string `json:"reason"`
	HTTPStatus  *int    `json:"http_status"`
	ProviderRef *string `json:"provider_ref"`
}

// ProvisionRequest は保管先準備アダプタの要求(op ごとに使う項目が違う)。
type ProvisionRequest struct {
	Contract            string                       `json:"contract"`
	Op                  string                       `json:"op"`
	Endpoint            string                       `json:"endpoint"`
	Bucket              string                       `json:"bucket"`
	Prefix              *string                      `json:"prefix,omitempty"`
	Versioning          *bool                        `json:"versioning,omitempty"`
	LockMode            string                       `json:"lock_mode,omitempty"`
	DefaultRetentionDay *int64                       `json:"default_retention_days,omitempty"`
	Roles               []string                     `json:"roles,omitempty"`
	MaxRetentionDays    *int64                       `json:"max_retention_days,omitempty"`
	KeyIDs              []string                     `json:"key_ids,omitempty"`
	KeyRole             *string                      `json:"key_role,omitempty"`
	Secrets             map[string]map[string]string `json:"secrets"`
}

// AdapterEnvPATH は子の PATH(spec 3.2.1 の「環境」)。固定の最小値で、核の PATH を引き継がない。
const AdapterEnvPATH = "/usr/local/bin:/usr/bin:/bin"

// AdapterEnv は子プロセスの環境を作る(spec 3.2.1 の「環境」、3.1.1 の規則 2)。
// 核の環境変数は引き継がず、PATH と LC_ALL=C のほかは extra のうち許可した名前だけを置く。
// 許可する名前は取得アダプタの SITATE_RECV、試験ビルドの核が渡す切り替えの変数(EnvTestB2AuthURL・EnvExtraCAFile)。
// プロキシの変数は渡さない。標準入力は要求 1 行の後に閉じ、記述子は 0・1・2 と check の入力 fd だけを継承する(呼び出し側の規則)。
func AdapterEnv(extra map[string]string) []string {
	env := []string{"PATH=" + AdapterEnvPATH, "LC_ALL=C"}
	for _, k := range AdapterEnvAllowed {
		if v, ok := extra[k]; ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// 子の環境に置ける名前。
const (
	EnvRecv          = "SITATE_RECV"
	EnvTestB2AuthURL = "SITATE_TEST_B2_AUTH_URL"
)

// AdapterEnvAllowed は AdapterEnv が extra から受ける名前(順序固定)。
var AdapterEnvAllowed = []string{EnvRecv, EnvTestB2AuthURL, EnvExtraCAFile}

// AdapterResource は restore-dest の resources[]・remaining[] の要素(type と id。ファイル名とパスは入れない)。
type AdapterResource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
