package contract

import (
	"context"
	"encoding/json"
)

// StateLockHandle は取得済みの state.lock の排他(解放の関数を持つ)。再入しない。
type StateLockHandle interface{ Release() }

// StateFiles は状態ファイル(提供: state)。state.json の更新と台帳の書き込みは排他の中での版だけを持つ。
type StateFiles interface {
	// Lock は文脈の締め切りで諦めたら LOCKED を返す(LOCK_UNAVAILABLE ではない)。
	Lock(ctx context.Context) (StateLockHandle, error)
	ReadDef(ctx context.Context) (normalized []byte, defHash string, err error)
	ReadCandidate(ctx context.Context) (normalized []byte, defHash string, found bool, err error)
	ValidateDef(ctx context.Context, def []byte, skipRefs bool) (normalized []byte, defHash string, issues json.RawMessage, err error)
	StageDef(ctx context.Context, normalized []byte) error // seal の前だけ
	Get(ctx context.Context, item string) (json.RawMessage, error)
	Update(ctx context.Context, held StateLockHandle, item string, v json.RawMessage) error
	SwitchDef(ctx context.Context, in SwitchInput) error
	LedgerAppend(ctx context.Context, held StateLockHandle, rec json.RawMessage) error
	LedgerList(ctx context.Context) ([]json.RawMessage, error)
	RecordReadStart(ctx context.Context, s ReadStart) error
	SettleInterrupted(ctx context.Context, runID string) error
	// LedgerDelete は台帳(ledger/)の記録 1 件(record_id)を消す。破棄を確かめられた(inspect が全資源 absent)後と、人の証言(attest --step dest_disposed)の後に使う
	// (spec 3.5 の表の 11・「人による残存の片付け」)。記録が無ければ何もしない。
	LedgerDelete(ctx context.Context, held StateLockHandle, recordID string) error
}

// HostKeyUpdate は承認済みの定義の切り替えに渡すホスト鍵の更新の要素。HostKeyFP が nil の要素は比べず更新しない。
type HostKeyUpdate struct {
	Role, Host string
	HostKeyFP  *string
}

// EnrollResult は登録の結果の状態の値。
type EnrollResult struct {
	ReceiverHash      string
	SourceClusters    json.RawMessage
	RestoreDestEnroll json.RawMessage
}

// SwitchInput は承認済みの定義の切り替えの入力。
type SwitchInput struct {
	Normalized []byte
	HostKeys   []HostKeyUpdate // 空なら所在・ホスト鍵に基づく索引の削除と host_keys[]・登録の結果の更新をしない
	Enroll     *EnrollResult
}

// ReadStart は読み出しの開始の記録。
type ReadStart struct {
	RunID, Kind, Month string
	Bytes              int64
}

// Secrets は参照名による秘密の取得・保存・削除(提供: cli)。値はログと結果に出ない。
type Secrets interface {
	Get(ctx context.Context, ref string) (json.RawMessage, error)
	Put(ctx context.Context, ref string, v json.RawMessage) error
	Delete(ctx context.Context, ref string) error
}

// AdapterRunner はアダプタの実行器(提供: adapter、利用: acquire・restore・keys・state)。
type AdapterRunner interface {
	Declaration(ctx context.Context, kind, name string) (Declaration, error)
	// Run は要求 JSON を渡して結果 JSON を返す。取得アダプタは結果の前に onFrame へストリームのフレームを渡す
	// (背圧: onFrame が返るまで次を読まない。途中で子が終わったら onFrame の後に ErrAdapterExited 相当の *Error を返す)。
	// 取得アダプタの end のフレームも onFrame に渡す(呼び出し側が終わりの鍵を照らす)。
	// アダプタの終了 1 は結果として返さず必ず誤りで返し、どの経路でも Error.Adapter に error の全体を載せる。NOT_IMPLEMENTED・INTERNAL 以外の
	// error.code は ADAPTER_ERROR の *Error(detail.adapter_code に error.code)。誤りが nil の結果は終了 0 だけ。
	Run(ctx context.Context, rc RunContext, kind, name string, req json.RawMessage, onFrame func(Frame) error) (json.RawMessage, error)
}

// Declaration は検証済みの adapter.json の内容。
type Declaration struct {
	Kind, Name, Version string
	Raw                 json.RawMessage
}

// Frame は sitate.stream/v1 のフレーム。
type Frame struct {
	Type string
	Meta json.RawMessage
	Data []byte
}

// RunCleanup は run_id 指定の片付け(提供: acquire・restore、利用: cli)。
type RunCleanup interface {
	Cleanup(ctx context.Context, own string, interrupted string) (cleaned, unverified []Resource, err error)
}

// Enroller は登録の実行(提供: restore・acquire、利用: approval)。
type Enroller interface {
	Enroll(ctx context.Context, in EnrollInput) (EnrollResult, error)
}

// EnrollInput は登録の入力。WriteState が偽のときは書かずに返し、cli が登録の結果を切り替えへ渡す。
type EnrollInput struct {
	Kind       string // source | restore-dest
	Section    json.RawMessage
	HostKeyFP  string
	WriteState bool
	// Host は kind: source の接続先(ssh://<user>@<host>:<port>。seams.go の ArgHost と同じ形)。restore-dest は Section.dest.at。
	Host string
	// AdminRef は管理用資格情報の参照名、Admin はその値(OpenSSH 形式の秘密鍵の文字列。cli が Call.Secrets か Secrets から引いて渡す。apply の enroll の要素では
	// アプリが差し込んだ値)。値はログと結果に出ない。
	AdminRef string
	Admin    *Secret
	// Rotate は `source enroll --rotate`(新しい pull 用鍵を作り、受け口の authorized_keys の行を置き換える)。
	Rotate bool
}

// Surveyor は元サーバーの調査と候補の判定(提供・利用: acquire)。状態ファイルへは書かない。
type Surveyor interface {
	Survey(ctx context.Context, scope string) (json.RawMessage, error) // scope: full | db_only。sitate.source-survey/v1
	Judge(ctx context.Context, survey json.RawMessage, def []byte) (class json.RawMessage, err error)
	SelectHostKey(ctx context.Context, in HostKeyChoiceInput) (HostKeyChoice, error)
}

// HostKeyChoiceInput は照らすホスト鍵の値の選択の入力。
type HostKeyChoiceInput struct {
	Host, Timing, DefHost string
	Sealed                bool
	SourceSeen            []string
	HostKeys              []string
	ProposalFP            string
}

// HostKeyChoice は選択の結果。Compare が偽なら照らさない。
type HostKeyChoice struct {
	Compare bool
	FP      string
	Err     *Error
}

// Proposer は提案の作成(提供: approval、利用: keys)。同じ内容の保留中の提案があればその id を返す。
type Proposer interface {
	Propose(ctx context.Context, kind string, content json.RawMessage) (changeID string, err error)
}

// PendingList は保留中の変更の一覧(提供: approval、利用: state)。
type PendingList interface {
	List(ctx context.Context) ([]json.RawMessage, error)
}

// ReadPlan は読み出しの計画の量(提供: restore、利用: state)。
type ReadPlan interface {
	Plan(ctx context.Context) (PlanAmount, error)
}

// PlanAmount は計画の量・その月の累計・予約の有無。
type PlanAmount struct {
	PlannedBytes, MonthToDateBytes int64
	Reserved                       bool
}

// Approval は承認と遅延の入口(提供: approval、利用: cli)。登録以外の操作は実行しない。
type Approval interface {
	Propose(ctx context.Context, c Call) (Reply, error)
	Approve(ctx context.Context, c Call) (Reply, error)
	// ApplyPlan は適用の計画を返す。ApplyEnroll は kind: enroll の要素の実行。ApplyRecord は各要素の後に結果を記録する。
	ApplyPlan(ctx context.Context, c Call, instance string) (ApplyPlan, error)
	ApplyEnroll(ctx context.Context, changeID string, pos int) (StepResult, error)
	ApplyRecord(ctx context.Context, changeID string, pos int, r StepResult) error
	Cancel(ctx context.Context, c Call) (Reply, error)
	Attest(ctx context.Context, c Call) (Reply, error)
	Pending(ctx context.Context, c Call) (Reply, error)
	// Gate は許可リストと --secret-fd の読み替えの判定。実行は cli。
	Gate(ctx context.Context, line string) (GateDecision, error) // line は SSH_ORIGINAL_COMMAND の字面(字句規則 spec 3.1.5 も判定の対象)
}

// Acquire は取得の流れの入口(提供: acquire、利用: cli)。
type Acquire interface {
	Selftest(ctx context.Context) (Reply, error) // 取得の前の自己診断(受け口に届かなければ STORAGE_UNREACHABLE)
	Run(ctx context.Context, c Call) (Reply, error)
	SourceEnroll(ctx context.Context, c Call) (Reply, error)
	SourceCheck(ctx context.Context, c Call) (Reply, error)
	SourceSurvey(ctx context.Context, c Call) (Reply, error)
}

// Restore は復元と復元テストの入口(提供: restore、利用: cli)。
type Restore interface {
	List(ctx context.Context, c Call) (Reply, error)
	RestoreTest(ctx context.Context, c Call) (Reply, error)
	Restore(ctx context.Context, c Call) (Reply, error) // イテレーション 1 は (a) の --to <空のディレクトリ>・--endpoint・--bucket・--prefix・--snapshot-key-stdin と materialize(spec 8.2)
	RestoreDestEnroll(ctx context.Context, c Call) (Reply, error)
	RestoreDestCheck(ctx context.Context, c Call) (Reply, error)
	RestoreVerify(ctx context.Context, c Call) (Reply, error)
	JournalVerify(ctx context.Context, c Call) (Reply, error) // 後のイテレーション(イテレーション 2)
}

// Keys は鍵管理の入口(提供: keys、利用: cli)。フレーズは Reply.Phrase に秘密の型で返す。
type Keys interface {
	Init(ctx context.Context, c Call) (Reply, error)
	InitStorage(ctx context.Context, c Call) (Reply, error)
	BucketCheck(ctx context.Context, c Call) (Reply, error)
	PhraseVerify(ctx context.Context, c Call) (Reply, error)
	StorageIssueReadKey(ctx context.Context, c Call) (Reply, error) // 後のイテレーション(イテレーション 2)
	StorageRevokeReadKey(ctx context.Context, c Call) (Reply, error)
	Kit(ctx context.Context, c Call) (Reply, error)
	KitConfirm(ctx context.Context, c Call) (Reply, error)
	Doctor(ctx context.Context, c Call) (Reply, error)
	SecretsSet(ctx context.Context, c Call) (Reply, error) // 後のイテレーション(イテレーション 3)
}
