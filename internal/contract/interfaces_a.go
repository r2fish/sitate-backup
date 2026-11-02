package contract

import (
	"context"
	"io"
	"time"
)

// 以下は spec 7.5 のインターフェースの表。名前・提供する部品・利用する部品は表のとおり(作業指示書 5.1 の表)。
// 7 章の機能(後のイテレーション)の操作には「後のイテレーション」の印を付ける。
// ObjectStore と Clock と RetentionParams は storage.go・clock.go。

// Retention は保管先の保持の操作(提供: store、利用: cli)。
type Retention interface {
	Verify(ctx context.Context, recorded []VersionRef) (VerifyResult, error)
	Extend(ctx context.Context, target string) (ExtendResult, error) // target: "A" | "B"
	Prune(ctx context.Context) (PruneResult, error)
	// HoldSet は削除停止を掛ける(spec 4.2。掛かっていれば since を変えず causes[] に足す)。since を返す。
	HoldSet(ctx context.Context, in HoldInput) (time.Time, error)
	// Class は保持区分の判定(spec 4.2「保持区分」)。timezone の現地の暦月に同じ月の monthly が無く、その世代が健全なら monthly。
	// timezone が読めなければ誤り(INVALID_ARGUMENT 相当ではなく設定の誤りとして呼び出し側が扱う)。
	Class(in ClassInput) (RetentionClass, error)
	// MissingRefs は参照の存在の照合(backup の終わり)。
	MissingRefs(ctx context.Context, refs []VersionRef) ([]VersionRef, error)
}

// HoldInput は HoldSet の入力: state.json の hold.causes[] の 1 件(schema/cli/results.json の hold_cause。at は呼び出し時刻で核が入れる)。
type HoldInput struct {
	Reason string   // check_failed | check_error | no_healthy_backup | restore_failed | manual
	Code   *Code    // 原因コード(manual は nil)
	Days   *int     // no_healthy_backup の auto_hold_days など(無ければ nil)
	Refs   []string // restore_failed で印を付けた世代の manifest_id(無ければ空)
}

// ClassInput は Class の入力。Snapshots は state.json の snapshots[] の、判定に要る項目(created_at・retention_class)だけ。
type ClassInput struct {
	CreatedAt time.Time // 判定する世代の created_at(月をまたぐ実行は終わりでなく created_at の月に数える)
	Healthy   bool      // 健常性 pass かつ全対象 ok
	Timezone  string    // IANA の名前(定義ファイルの timezone。4.1)
	Snapshots []SnapshotClassRef
}

// SnapshotClassRef は Class の入力に使う既存の世代の 1 件。
type SnapshotClassRef struct {
	CreatedAt      time.Time
	RetentionClass RetentionClass
}

// VersionRef は記録した版 ID つきのキー。Size・ETag・RetainUntil は記録した値(verify の入力。spec 4.2)で、ゼロなら照合しない。
type VersionRef struct {
	Key, VersionID string
	Size           int64
	ETag           string
	RetainUntil    time.Time
	WrittenAt      time.Time // 書き込み時刻(記録にあれば。無ければゼロ)
}

// RetentionMismatch は verify の retention_short[]・retention_long[] の要素(spec 3.1.2「結果の配列の要素の形」)。
type RetentionMismatch struct {
	Key, VersionID string
	Recorded       time.Time // 記録した retain-until
	Actual         time.Time // 保管先から版指定で読んだ retain-until
}

// ExtendFailure は extend の failed[] の要素。
type ExtendFailure struct {
	Key, VersionID string
	Code           Code // 原因コード
}

// VerifyResult は verify の結果(spec 3.1.2 の verify の行)。
type VerifyResult struct {
	Missing                           []VersionRef
	RetentionShort, RetentionLong     []RetentionMismatch
	UnexpectedVersions                []VersionRef
	NoncurrentVersions, DeleteMarkers int
	BytesWrittenVsBucket              float64 // 書いた量と保管先の量の比(spec 4.2)
}

// ExtendResult は extend の結果。
type ExtendResult struct {
	Extended       int
	Failed         []ExtendFailure
	MinRetainUntil time.Time
}

// PruneResult は prune の結果。
// Held は削除停止中で何も消さなかったこと(results.json の prune.held は真偽値)。
type PruneResult struct {
	Deleted, SkippedLocked, Protected int
	Held                              bool
}

// StoreProbe は probe/ への書き込みと禁止の操作の試行(提供: store、利用: keys の bucket check)。
type StoreProbe interface {
	PutProbe(ctx context.Context, key string, body []byte) (versionID string, err error)
	Try(ctx context.Context, role, op string) (allowed bool, err error)
}

// Codec は保管形式(提供: format)。
type Codec interface {
	Chunk(ctx context.Context, r io.Reader, emit func(Chunk) error) error
	Seal(plain, chunkKey []byte, chunkID string) ([]byte, error)
	Open(sealed, chunkKey []byte, chunkID string) ([]byte, error)          // AAD にチャンク ID を含む(spec 4.3 の AAD の表)ため、封緘と同じ ID を受ける
	Encode(kind, id string, h Header, content, key []byte) ([]byte, error) // kind: 4 バイトの種類(MagicManifest など)。id は AAD の ID(AAD の説明を見よ)
	Decode(kind, id string, sealed, key []byte) ([]byte, error)            // 見出しは sealed から読む。開けない理由は *FormatError
	Header(b []byte) (Header, error)
	// AAD の id(spec 4.3 の AAD の表): SITS は状態ディレクトリからの相対パス(UTF-8。AAD では長さ 2 バイト ∥ UTF-8)、それ以外は ID の生のバイト列の小文字 16 進:
	// SITC・WRPC=チャンク ID(32 バイト)、SITM・WRPS=manifest_id の "m-" を除いた乱数部(16 バイト = 32 桁)、SITG=def_hash の SHA-256 の値(32 バイト)、
	// SITJ=seq(8 バイトのビッグエンディアン = 16 桁)、SITK=keyring_id の "kr-" を除いた乱数部(16 バイト)。
	AAD(kind, id string, h Header) ([]byte, error)
	Hash(content []byte) string // BLAKE3
	// HashStream は Hash と同じ値を、全体をメモリに載せずに計算する(大きなファイルの照合)。
	HashStream(r io.Reader) (string, error)
}

// Chunk はチャンク化の出力。
type Chunk struct {
	Data   []byte
	Length int
}

// Header は見出しの読み取り結果。
type Header struct {
	Kind          string // 4 バイトの種類(MagicChunk など)
	FormatVersion int
	Mark          []byte // 見出しの印(spec 4.3 の AAD の表の「見出しの印」)
	Wrapped       []byte // マニフェストだけ: 見出しに置く包んだスナップショット鍵(nonce ∥ 暗号文 ∥ タグ)
}

// FormatError は Codec が開けないオブジェクトの理由を分けて返す型付きの誤り(spec 4.3「読めないときの扱い」)。
// Reason は header(印が種類と違う)/ tag(認証付き復号の失敗)/ too_new / too_old。too_new は Required と Supported を持つ。
type FormatError struct {
	Reason    string
	Required  int
	Supported []int
}

func (e *FormatError) Error() string { return "format: " + e.Reason }

// Crypto は暗号部品(提供: format、利用: restore・keys)。
type Crypto interface {
	AEADSeal(plain, key, aad []byte) ([]byte, error)
	AEADOpen(sealed, key, aad []byte) ([]byte, error)
	HKDF(ikm, info []byte, n int) ([]byte, error)
	Argon2id(phrase, salt []byte, memKiB, iterations, parallelism int) ([]byte, error)
	BLAKE3(content []byte) []byte
	SignProof(record []byte, key []byte) ([]byte, error)
	VerifyProof(record, sig, pub []byte) (bool, error)
}

// IssuedKey は issue-keys の issued[] の 1 要素(spec 3.2.6)。Secret は *Secret なので String・MarshalJSON は伏せる。
type IssuedKey struct {
	Role   string
	KeyID  string
	Secret *Secret
}

// KeyringInfo は開けたキーリングの要約(OpenedKeyring)。
type KeyringInfo struct {
	Version   int
	KeyringID string // kr-<32 桁>
	RepoID    string
	KeyIDs    []string
}

// IssueRequest は issue-keys の 1 回の要求(spec 3.2.6)。Prefix は保管先のキーの接頭辞 `<p>` をそのまま(アダプタの要求の `prefix`。store が付ける `<p>` と同じ値)。
// Admin は事業者の管理用鍵(--admin-ref の値。Call.Secrets から)で、アダプタの要求の secrets に入れる。
type IssueRequest struct {
	Endpoint, Bucket, Prefix string
	Roles                    []string
	MaxRetentionDays         int
	Admin                    *Secret
}

// RevokeRequest は revoke-keys の 1 回の要求(spec 3.2.6)。KeyIDs は失効させる事業者の鍵の識別子。
// Roles は任意(空ならアダプタの要求に入れない。`storage revoke-read-key` は read)。Admin は IssueRequest と同じく事業者の管理用鍵。
type RevokeRequest struct {
	Endpoint, Bucket string
	KeyIDs           []string
	Roles            []string
	Admin            *Secret
}

// KeyService は鍵(提供: keys)。マスター鍵の値は境界を越えない。
type KeyService interface {
	NewSnapshotKey(ctx context.Context, manifestID string) (key, wrapped []byte, keyID string, err error) // manifestID は "m-" 付きの識別子。包み(WRPS)の AAD の ID はその乱数部 16 バイト(spec 4.3)
	OpenSnapshotKey(ctx context.Context, wrapped []byte, keyID, manifestID string) ([]byte, error)
	OpenKeyring(ctx context.Context, phrase *Secret, keyring []byte) (bool, error)
	// OpenedKeyring は phrase で開いたキーリングの中身の要約を返す。開けなければ PHRASE_INVALID。
	// Version は `keyring_version`(1 から始まり確定のたびに 1 増える。spec 4.3)で、`init` の秘密のレコード・`phrase verify` の結果と同じ値。
	// KeyIDs は含まれるマスター鍵の鍵 ID(`last_phrase_verify.key_ids[]` と復元テストの表の 1 の入力)。
	OpenedKeyring(ctx context.Context, phrase *Secret, keyring []byte) (KeyringInfo, error)
	// IssueKeys は保管先準備アダプタの issue-keys を 1 回呼ぶ(req.Roles の順に発行する)。発行は全部か無しか(spec 3.2.6): 一部でも発行できなければ
	// この呼び出しで発行した鍵を失効させてから error を返し、失効にも失敗した鍵の ID を orphaned に返す(アダプタの error.orphaned_key_ids[] と、核の側の失効の失敗の両方)。
	// 返った Secret は呼び出し側が Secrets へ置いて Wipe する(結果・封筒・ログへ出さない。spec 3.1.4)。
	IssueKeys(ctx context.Context, req IssueRequest) (issued []IssuedKey, orphaned []string, err error)
	// RevokeKey は保管先準備アダプタの revoke-keys を 1 回呼ぶ(spec 3.2.6)。存在しない key_id は notFound に入れて成功(冪等)。
	// 製品の印の無い鍵・管理用鍵そのもの・Roles を付けたときにその役割でない鍵が 1 つでもあれば、1 件も失効させずに ARG_INVALID を返す(アダプタの事前の検査)。
	RevokeKey(ctx context.Context, req RevokeRequest) (revoked, notFound []string, err error)
	ChunkID(plain []byte) string
	EntryRef(path string) string
	CandidateFingerprint(kind, path string) string
	WrapChunkKey(chunkKey []byte, chunkID string) ([]byte, error)
	UnwrapChunkKey(wrapped []byte, chunkID string) ([]byte, error)
	DerivedKey(purpose string) ([]byte, error) // config | journal | state | proof
}

// Verifier は署名の検証(提供: keys、利用: adapter・acquire・state・cli)。検証する鍵は TrustAnchors による。
type Verifier interface {
	Verify(ctx context.Context, target, sig []byte, want TrustedComment) (bool, error)
}

// TrustedComment は trusted comment の期待値(version・arch・path の 3 つ)。
type TrustedComment struct{ Version, Arch, Path string }

// Index は参照チャンクの索引(提供: acquire)。
type Index interface {
	Lookup(ctx context.Context, chunkID string) (*ChunkRef, error)
	Reserve(ctx context.Context, ref ChunkRef) error // 先行書き込み。版 ID は送信の応答の後に追記
	SetVersion(ctx context.Context, chunkID, versionID string) error
	// SetRetain は版の retain-until の記録を更新する(書き込みの応答と延長の後。spec 4.3「索引と prune の整合」、4.5 の index/chunks)。
	// 重複排除の判定はこの記録の値で行い、保管先から読み直さない。
	SetRetain(ctx context.Context, versionID string, until time.Time) error
	// Records は版 ID のある全チャンクの記録(verify の照合と extend の入力。Key は chunks/ のキー)。
	Records(ctx context.Context) ([]VersionRef, error)
	Rebuild(ctx context.Context, manifests [][]byte, against []byte) error // 比べる定義を入力に受ける
	Remove(ctx context.Context, versionID string) error
}

// Lease は実行の排他(提供・利用: cli だけ)。
type Lease interface {
	Acquire(ctx context.Context) (acquired bool, err error)
	WriteMarker(ctx context.Context, marker []byte) (prev []byte, err error)
	Deadline(ctx context.Context) (context.Context, context.CancelFunc)
}

// Recorder は journal/ の記録と通知イベントの発出(提供: state)。
type Recorder interface {
	Record(ctx context.Context, entry []byte) (prevHash string, err error)
	// AppendLocked は state.lock を持つ呼び出し側が使う排他の中での追記(再入しない)。
	AppendLocked(ctx context.Context, held StateLockHandle, entry []byte) (string, error)
	WriteRemote(ctx context.Context, item []byte) error
	Emit(ctx context.Context, typ string, facts []byte) (Event, []DeliveryResult, error)
}

// Event は sitate.event/v1 の値。
type Event struct {
	ID       string
	Type     string
	Facts    []byte
	Severity string    // info | warn | critical(spec 4.8)
	Time     time.Time // 発生時刻
	// RepoID・SuppressedCount・Next は sitate.event/v1 の必須項目(schema/data/event.json)。通知アダプタへ渡す全体を Notifier が組み立てるために運ぶ。
	RepoID          *string // rp-<32 桁>。リポジトリの無い段階は nil(JSON の null)
	SuppressedCount int     // 同種の抑止した件数
	Next            []Next  // 次の操作(無ければ空の配列。nil にしない)
}

// DeliveryResult は宛先ごとの配送の結果(spec 4.8)。
type DeliveryResult struct {
	To         string
	Channel    string
	Delivered  bool
	Attempts   int
	Reason     string
	AcceptedAt *time.Time
}

// Notifier は通知の配送と送り直し(提供: state)。
type Notifier interface {
	Deliver(ctx context.Context, ev Event, to []string) ([]DeliveryResult, error)
	Resend(ctx context.Context) ([]DeliveryResult, error)
}

// TestNotifier は試験通知(提供: state、利用: approval)。確認語のハッシュだけを残す。
type TestNotifier interface {
	// proofHashes は宛先 → `<word_salt>|<word_hash>`(spec 4.5。approval が change_proofs[] に写す)。確認語そのものは返さない。
	Test(ctx context.Context, to []string) (proofHashes map[string]string, err error)
}

// Timers はタイマーの登録(提供: state、利用: keys の init)。
type Timers interface {
	Register(ctx context.Context, units []TimerUnit) error
	SetOuterLimit(ctx context.Context, limit time.Duration) error // 上限 + 猶予 + 記録の締め切り
}

// TimerUnit は定期実行の 1 単位。
type TimerUnit struct {
	Words  []string
	Period string // hourly | daily | weekly | monthly
}
