package contract

import (
	"encoding/json"
	"time"
)

// Warning は warnings[] の要素(spec 3.1.1)。
type Warning struct {
	Code   Code            `json:"code"`
	Detail json.RawMessage `json:"detail"`
	Next   []Next          `json:"next"`
}

// Next は next[] の要素。Why は原因コードか nil。
type Next struct {
	Argv             []string `json:"argv"`
	RequiresHuman    bool     `json:"requires_human"`
	RequiresApproval bool     `json:"requires_approval"`
	Why              *Code    `json:"why"`
}

// Error は封筒の error(spec 3.1.1)。部品は型付きの Error を返し、cli が封筒にする。核は人向けの文を持たない。
type Error struct {
	Code      Code            `json:"code"`
	Class     string          `json:"class"`
	Retryable bool            `json:"retryable"`
	Detail    json.RawMessage `json:"detail"`
	Next      []Next          `json:"next"`
}

func (e *Error) Error() string { return string(e.Code) }

// Call は部品のサブコマンドの入口への呼び出し(3.1.2 の表の 1 行)。Args は引数の名前(`--` を除く)から値。
type Call struct {
	Words []string
	// Positional は位置引数(3.1.5 の字句規則 4。`propose <種別>` の種別、`secrets set <参照名>` の参照名など。Words はサブコマンドの語だけ)。
	Positional []string
	Args       map[string][]string
	DryRun     bool
	RunID      string
	Secrets    SecretInput
	// SecretOut はフレーズなど専用出力経路へ出す秘密の出力先があるか(cli が渡す。無ければ SECRET_FD_REQUIRED を生成の前に返す)。
	SecretOut bool
	// PhraseIn は --phrase-stdin(`phrase verify`・`init --adopt`)で受けた標準入力の秘密(spec 3.1.1「秘密の入力の規則」。--secrets-stdin と併用なら 2 行目、単独なら 1 行目)。
	// 値は結果・ログ・誤りの detail に出さない。使い終えたら呼び出し側が Wipe する。無ければ nil。
	// Args のキーと Secrets の形は seams.go(ArgHost・ArgAdminRef ほか、SecretKind*)に固定した。
	PhraseIn *Secret
	// SnapshotKeyIn は --snapshot-key-stdin で受けた標準入力の 1 行目(DestKeyLine の JSON。spec 3.5 の表の 7・8)。扱いは PhraseIn と同じ。無ければ nil。
	// 標準入力の終わり(取り消し)は cli が ctx の取り消しで渡す。入口は読み出しを止めて終わる。
	SnapshotKeyIn *Secret
}

// Reply は入口の結果。Result は JSON の値(スキーマは schema/cli/results.json)。Phrase は専用出力経路へ出す秘密(cli だけが書いて消す)。
type Reply struct {
	Result   json.RawMessage
	Warnings []Warning
	Phrase   *Secret
}

// Secret は秘密の値の型。ログと結果に出ない(String/MarshalJSON は伏せる)。
type Secret struct{ b []byte }

// NewSecret は値を包む。
func NewSecret(b []byte) *Secret { return &Secret{b: append([]byte(nil), b...)} }

// Reveal は値を返す(呼び出し側が書いたらすぐ Wipe する)。
func (s *Secret) Reveal() []byte { return s.b }

// Wipe は値をメモリから消す。
func (s *Secret) Wipe() {
	for i := range s.b {
		s.b[i] = 0
	}
	s.b = nil
}

// String は伏せる。
func (s *Secret) String() string { return "[secret]" }

// MarshalJSON は伏せる。
func (s *Secret) MarshalJSON() ([]byte, error) { return []byte(`"[secret]"`), nil }

// SecretInput は参照名から値(--secrets-stdin で受けたもの)。
type SecretInput map[string]json.RawMessage

// RunContext は実行の文脈(cli が AdapterRunner に渡す。spec 3.1.1 の規則 13)。
type RunContext struct {
	RunID        string
	ProcessGroup bool // 子を自分のプロセスグループで起動する
	PdeathSig    bool // PR_SET_PDEATHSIG を掛ける
	Deadline     time.Time
}

// ChunkRef はチャンクの参照(索引の問い合わせの結果)。
type ChunkRef struct {
	ChunkID          string
	VersionID        string
	WrappedKey       []byte
	FirstSeen        string // manifest_id(確定前は仮の値。spec 4.5)
	FirstSeenPending bool   // マニフェストを書かずに終わった実行の印(first_seen_pending)
	FormatVersion    int
	Size             int64
	ETag             string
	RetainUntil      time.Time
}

// OpStep は apply の操作の列の 1 要素(spec 7.5 の apply の入口)。
type OpStep struct {
	Kind  string          `json:"kind"`
	Entry string          `json:"entry"`
	Args  json.RawMessage `json:"args"`
}

// ChangeOps は変更 1 件の操作の列。
type ChangeOps struct {
	ChangeID string   `json:"change_id"`
	Op       string   `json:"op"`
	Steps    []OpStep `json:"steps"`
}

// ApplyPlan は apply の適用の計画(waiting[] と failed[] を含む)。
type ApplyPlan struct {
	Instance string
	Changes  []ChangeOps
	Waiting  []json.RawMessage
	Failed   []json.RawMessage
}

// StepResult は操作の列の 1 要素の結果(成功なら Result、失敗なら Err)。
type StepResult struct {
	Result json.RawMessage
	Err    *Error
}

// GateDecision は gate の判定結果(許可 / 拒否・実行する引数列・秘密の出力先)。
type GateDecision struct {
	Allowed   bool
	Argv      []string
	SecretOut string // "" | "stdout-first-record"
	Denied    *Error
}

// Resource は片付けた資源。
type Resource struct {
	Kind string
	ID   string
}
