package contract

// 通知イベントの種類(仕様書 4.8)。schema/data/event.json の type と一致する(試験で照合)。
// MergeKey は warn のまとめのキー(facts のキー名をコンマでつなぐ。"type" は type だけ、空は「まとめない」)。
// Resend は「次の実行での送り直し」の列(critical のすべてと change.*・restore_session.*)。
type EventType struct {
	Type       string
	Severities []string
	MergeKey   string
	Resend     bool
}

// EventTypes は 4.8 の表の全種類。
var EventTypes = []EventType{
	{Type: "backup.failed", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "backup.partial", Severities: []string{"warn", "critical"}, MergeKey: "type", Resend: true},
	{Type: "check.failed", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "check.error", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "verify.failed", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "storage.unexpected_version", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "storage.noncurrent_surge", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "storage.size_drift", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "storage.retention_long", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "journal.chain_broken", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "storage.unreachable", Severities: []string{"warn"}, MergeKey: "type", Resend: false},
	{Type: "keyring.mismatch", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "retention.extension_failed", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "restore_test.passed", Severities: []string{"info"}, MergeKey: "", Resend: false},
	{Type: "restore_test.failed", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "restore_test.overdue", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "restore_test.deferred", Severities: []string{"warn"}, MergeKey: "type", Resend: false},
	{Type: "restore_session.opened", Severities: []string{"warn"}, MergeKey: "session_id", Resend: true},
	{Type: "restore_session.expiring", Severities: []string{"warn"}, MergeKey: "session_id", Resend: true},
	{Type: "restore_session.closed", Severities: []string{"info"}, MergeKey: "session_id", Resend: true},
	{Type: "restore_session.released", Severities: []string{"warn"}, MergeKey: "session_id", Resend: true},
	{Type: "teardown.unverified", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "change.proposed", Severities: []string{"warn"}, MergeKey: "change_id", Resend: true},
	{Type: "change.approved", Severities: []string{"warn"}, MergeKey: "change_id", Resend: true},
	{Type: "change.applying_soon", Severities: []string{"warn"}, MergeKey: "change_id", Resend: true},
	{Type: "change.applied", Severities: []string{"info"}, MergeKey: "change_id", Resend: true},
	{Type: "change.expired", Severities: []string{"info"}, MergeKey: "change_id", Resend: true},
	{Type: "change.failed", Severities: []string{"warn", "critical"}, MergeKey: "change_id,state", Resend: true},
	{Type: "hold.set", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "notify.test", Severities: []string{"info"}, MergeKey: "", Resend: false},
	{Type: "kit.outdated", Severities: []string{"warn"}, MergeKey: "type", Resend: false},
	{Type: "phrase.unconfirmed", Severities: []string{"warn"}, MergeKey: "type", Resend: false},
	{Type: "kit.location_check_due", Severities: []string{"info"}, MergeKey: "", Resend: false},
	{Type: "run.interrupted", Severities: []string{"warn"}, MergeKey: "command", Resend: false},
	{Type: "run.stuck", Severities: []string{"critical"}, MergeKey: "holder_run_id", Resend: true},
	{Type: "time.skew", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "storage.permission_drift", Severities: []string{"critical"}, MergeKey: "", Resend: true},
	{Type: "cost.ceiling_exceeded", Severities: []string{"warn"}, MergeKey: "type", Resend: false},
	{Type: "source.uncovered", Severities: []string{"warn"}, MergeKey: "type", Resend: false},
	{Type: "report.monthly", Severities: []string{"info"}, MergeKey: "", Resend: false},
	{Type: "digest.weekly", Severities: []string{"info"}, MergeKey: "", Resend: false},
}

// 通知の送り直しの回数・間隔・期限は constants.go の定数表を使う。
