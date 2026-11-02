package acquire

// 骨格(作業指示書 5.1): 契約のインターフェースを満たし、NOT_IMPLEMENTED を返す。

import (
	"context"
	"encoding/json"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// Index は contract.Index の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// 依存は公開フィールドで受ける。Dir は状態ディレクトリ(索引は <Dir>/index/chunks に 1 記録ごと状態鍵で暗号化し、各記録の前に
// 4 バイトのビッグエンディアンの長さを置いて並べる。spec 4.5)、KeyS は状態鍵(DerivedKey("state"))を出す。
type Index struct {
	Dir   string
	Codec contract.Codec
	KeyS  contract.KeyService
}

var _ contract.Index = (*Index)(nil)

// Lookup は contract.Index を満たす。
func (*Index) Lookup(ctx context.Context, chunkID string) (*contract.ChunkRef, error) {
	var r0 *contract.ChunkRef
	return r0, contract.ErrNotImplemented("acquire.Index.Lookup")
}

// Reserve は contract.Index を満たす。
func (*Index) Reserve(ctx context.Context, ref contract.ChunkRef) error {
	return contract.ErrNotImplemented("acquire.Index.Reserve")
}

// SetVersion は contract.Index を満たす。
func (*Index) SetVersion(ctx context.Context, chunkID, versionID string) error {
	return contract.ErrNotImplemented("acquire.Index.SetVersion")
}

// Rebuild は contract.Index を満たす。
func (*Index) Rebuild(ctx context.Context, manifests [][]byte, against []byte) error {
	return contract.ErrNotImplemented("acquire.Index.Rebuild")
}

// Remove は contract.Index を満たす。
func (*Index) Remove(ctx context.Context, versionID string) error {
	return contract.ErrNotImplemented("acquire.Index.Remove")
}

// Acquire は contract.Acquire の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// 依存は公開フィールドで受ける(契約試験が偽物を差し込む。実装は他のフィールドを足してよいが、これらを消さない)。
type Acquire struct {
	Store    contract.ObjectStore
	State    contract.StateFiles
	Index    contract.Index
	Codec    contract.Codec
	KeyS     contract.KeyService
	Adapters contract.AdapterRunner
	Clock    contract.Clock
	// SSH は元サーバーの受け口(pull 用鍵、`sitate-ro`)と管理アクセスへの接続。Secrets は pull 用鍵(参照名 contract.SourcePullKeyRef)と管理用資格情報の取得・保存。
	SSH     contract.SSH
	Secrets contract.Secrets
}

var _ contract.Acquire = (*Acquire)(nil)

// Selftest は contract.Acquire を満たす。
func (*Acquire) Selftest(ctx context.Context) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("acquire.Acquire.Selftest")
}

// Run は contract.Acquire を満たす。
func (*Acquire) Run(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("acquire.Acquire.Run")
}

// SourceEnroll は contract.Acquire を満たす。
func (*Acquire) SourceEnroll(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("acquire.Acquire.SourceEnroll")
}

// SourceCheck は contract.Acquire を満たす。
func (*Acquire) SourceCheck(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("acquire.Acquire.SourceCheck")
}

// SourceSurvey は contract.Acquire を満たす。
func (*Acquire) SourceSurvey(ctx context.Context, c contract.Call) (contract.Reply, error) {
	var r0 contract.Reply
	return r0, contract.ErrNotImplemented("acquire.Acquire.SourceSurvey")
}

// Surveyor は contract.Surveyor の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
// Conn と SSH は調査の接続(Survey の引数には接続先が無いため、接続ごとに組む)。
type Surveyor struct {
	SSH  contract.SSH
	Conn contract.SSHConn
}

var _ contract.Surveyor = (*Surveyor)(nil)

// Survey は contract.Surveyor を満たす。
func (*Surveyor) Survey(ctx context.Context, scope string) (json.RawMessage, error) {
	var r0 json.RawMessage
	return r0, contract.ErrNotImplemented("acquire.Surveyor.Survey")
}

// Judge は contract.Surveyor を満たす。
func (*Surveyor) Judge(ctx context.Context, survey json.RawMessage, def []byte) (class json.RawMessage, err error) {
	var r0 json.RawMessage
	return r0, contract.ErrNotImplemented("acquire.Surveyor.Judge")
}

// SelectHostKey は contract.Surveyor を満たす。
func (*Surveyor) SelectHostKey(ctx context.Context, in contract.HostKeyChoiceInput) (contract.HostKeyChoice, error) {
	var r0 contract.HostKeyChoice
	return r0, contract.ErrNotImplemented("acquire.Surveyor.SelectHostKey")
}

// RunCleanup は contract.RunCleanup の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
type RunCleanup struct{}

var _ contract.RunCleanup = (*RunCleanup)(nil)

// Cleanup は contract.RunCleanup を満たす。
func (*RunCleanup) Cleanup(ctx context.Context, own string, interrupted string) (cleaned, unverified []contract.Resource, err error) {
	var r0 []contract.Resource
	var r1 []contract.Resource
	return r0, r1, contract.ErrNotImplemented("acquire.RunCleanup.Cleanup")
}

// SourceEnroller は contract.Enroller の骨格。実装は WP が書く(いまは NOT_IMPLEMENTED を返す)。
type SourceEnroller struct {
	SSH     contract.SSH
	Secrets contract.Secrets
	State   contract.StateFiles
}

var _ contract.Enroller = (*SourceEnroller)(nil)

// Enroll は contract.Enroller を満たす。
func (*SourceEnroller) Enroll(ctx context.Context, in contract.EnrollInput) (contract.EnrollResult, error) {
	var r0 contract.EnrollResult
	return r0, contract.ErrNotImplemented("acquire.SourceEnroller.Enroll")
}
