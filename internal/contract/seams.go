package contract

import (
	"context"
	"encoding/json"
	"io"
)

// 継ぎ目の追加(spec 7.5「表に無い継ぎ目が実装で要ったときは U0 が contract にインターフェースと偽物を足す」)。
// 型・インターフェース・定数だけで処理を持たない。偽物は contract/fake の ssh.go・seams.go。

// --- SSH の継ぎ目(提供: cli が結合で実物を注入。U1c の元サーバーへの接続、U1e の復元先への接続が使う) ---

// SSH の接続の失敗の理由(SSHError.Reason)。
const (
	SSHReasonConnect = "connect" // 届かない・名前解決・タイムアウト
	SSHReasonAuth    = "auth"    // 資格情報の拒否

	// SSHReasonKeyUnparsable は Cred が鍵として読めない(パスフレーズ付きを含む)。
	SSHReasonKeyUnparsable = "key_unparsable"
)

// SSHTarget は接続先。Port が 0 なら 22。
type SSHTarget struct {
	Host string
	Port int
	User string
}

// SSHConn は接続の指定(接続先・資格情報・固定する指紋)。
//   - 資格情報: CredRef は参照名(記録と誤りの文に使う)、Cred は核が Call.Secrets か Secrets から引いた値(OpenSSH 形式の秘密鍵の文字列。ログと結果に出ない)。
//   - ホスト鍵: PinnedFP が空でなければ、見えた指紋がそれと違うとき何も実行せず(標準入力も読まず)*SSHHostKeyError を返す。空なら照らさず、見えた指紋を SSHResult.SeenFP に返す。
type SSHConn struct {
	Target   SSHTarget
	CredRef  string
	Cred     *Secret
	PinnedFP string
}

// SSHRequest は 1 回の実行(1 接続・1 コマンド)。
//   - 実行するもの: Argv(引数列。実装は空白で結んだ 1 つのコマンド文字列として送る。受け口の強制コマンドが SSH_ORIGINAL_COMMAND で受ける形も同じ)か、
//     Script(台本。Sudo が偽なら `sh -s`、真なら `sudo -n sh -s` の標準入力へ流す。このとき Stdin は使わない)。どちらか一方。
//   - Sudo: 呼び出し側が決める。復元先の `sitate-restore` は root も sudo も持たないので偽で呼ぶ(spec 3.5)。元サーバーの管理アクセスは User が root でなければ真。Argv には効かない。
//   - 流れ: Stdin は標準入力(nil なら空)。Stdout へ標準出力を書く(nil なら捨てる)。Stdin を開いたまま実行を続けてよく(spec 3.5 の表の 8)、
//     文脈の取り消しか Stdin の終わりで終わる。
type SSHRequest struct {
	SSHConn
	Argv   []string
	Script []byte
	Sudo   bool
	Stdin  io.Reader
	Stdout io.Writer
}

// SSHResult は実行の結果。ExitStatus は元の終了状態(127 などもそのまま)、SeenFP は接続で見えたホスト鍵の指紋(SHA256:...)。
type SSHResult struct {
	ExitStatus int
	SeenFP     string
}

// SSH は SSH の実行(1 つのインターフェースにまとめた継ぎ目)。第 1 引数は context。
type SSH interface {
	Run(ctx context.Context, req SSHRequest) (SSHResult, error)
}

// SSHError は接続できなかった・認証が通らなかったときの誤り。原因コードへの写像は呼び出し側が行う
// (元サーバーなら STORAGE_UNREACHABLE の target: source、復元先なら target: restore_host など)。
type SSHError struct{ Reason string }

func (e *SSHError) Error() string { return "ssh: " + e.Reason }

// SSHHostKeyError は見えた指紋が PinnedFP と違ったときの誤り。原因コード(SOURCE_HOSTKEY_MISMATCH / RESTORE_HOSTKEY_MISMATCH)への写像は呼び出し側が行い、
// detail の expected_fp に Pinned を、seen_fp に Seen を入れる。
type SSHHostKeyError struct{ Pinned, Seen string }

func (e *SSHHostKeyError) Error() string { return "ssh: host key mismatch" }

// 核が生成する秘密の予約名(spec 4.5「参照名と秘密ファイル」。定義と引数で人とエージェントが使うと拒む)。値は kind: ssh_key(SSHKeySecret)。
const (
	// SourcePullKeyRef は元サーバーの pull 用鍵(`sitate-ro` で入る。`source enroll`)の参照名。
	SourcePullKeyRef = "sitate-pull"
	// RestoreKeyRef は復元先用 SSH 鍵(`restore-dest enroll`)の参照名。
	RestoreKeyRef = "sitate-restore"
)

// --- 配布物(提供: cli が結合で実物を注入。U1e の核とチェックの配置、U1g の bin/ の書き込みが使う) ---

// BundleFile は配布物 bundle/<version>/ の 1 ファイル。Path は版ディレクトリからの相対パス(`linux-x86_64/sitate` の形。spec 4.2 の `<os>-<arch>` を頭に持つ)。
// Sig は署名ファイル(minisign)、Want は trusted comment の期待値(署名の検証に使う。復元先への配置では使わない)。
type BundleFile struct {
	Path string
	Data []byte
	Sig  []byte
	Want TrustedComment
}

// BundleSource は配布物の読み出し。osArch は "<os>-<arch>"(空なら全部)。保管先の bin/ からは読まない(spec 3.5 の表の 6)。
type BundleSource interface {
	Files(ctx context.Context, osArch string) (version string, files []BundleFile, err error)
}

// --- OS の検査(提供: cli が結合で実物を注入。U1g の doctor の bootstrap 段階が使う。statfs などの差し替え口) ---

// OSProbe は doctor の bootstrap 段階の実測。
type OSProbe interface {
	// FSType は path を載せるファイルシステムの種類(statfs の結果を名前にしたもの: ext4・xfs・nfs・cifs・fuse など)。
	FSType(ctx context.Context, path string) (string, error)
	// Check は DoctorChecks の項目 name(state_dir_fs を除く。bootstrap・sealed の項目と advisory の disk_encryption・swap_disabled)の実測。ok が偽なら detail に原因の材料を入れる。
	// check_images は実測せず、対象 0 件で合格とする(spec 3.1.2。DoctorCheckImagesCount)。
	Check(ctx context.Context, name string) (ok bool, detail json.RawMessage, err error)
}

// 状態ディレクトリに置けないファイルシステムの種類(spec 3.1.1 の規則 19。FSType の値と比べる)。
var FSTypesStateForbidden = []string{"nfs", "nfs4", "cifs", "smb", "smb2", "fuse", "fuseblk"}

// --- keys/ の配置(提供: keys が状態ディレクトリの下で自分で読み書きする。マスター鍵の値は keys の外へ出ない。spec 4.5 の `keys/` の配置) ---
// cli が keys に渡すのは状態ディレクトリの位置(keys.Keys.Dir・keys.KeyService.Dir)だけで、keys/ の中身(マスター鍵・保留)の受け渡しは契約に無い。
// 下の名前は状態ディレクトリからの相対パス。保留(pending)は init と phrase-reissue・key-rotate が置き、phrase verify の確定が消す。
const (
	KeysMasterFile    = "keys/master.json"
	KeysKeyringLatest = "keys/keyring.latest"
	KeysRepoLatest    = "keys/repo.latest"
	KeysPendingDir    = "keys/pending"
	// 保留の中身(KeysPendingDir からの相対)。repo.json は spec 4.5 の列挙に無く U0 が足した(init が保管先へ書く repo.json を確定まで持つ場所)。
	KeysPendingKeyring = "keyring"
	KeysPendingMaster  = "master.json"
	KeysPendingMeta    = "pending.json" // {"op","change_id"}
	KeysPendingRepo    = "repo.json"
)

// --- HostKeyChoiceInput.Timing の値 ---

// HostKeyChoiceInput.Timing の値(spec 3.3 の「ホスト鍵」)。
const (
	HostKeyTimingSurvey = "survey"
	HostKeyTimingEnroll = "enroll"
	HostKeyTimingApply  = "apply"
)

// HostKeyTimings は Timing の許される値(これ以外の値の入力は呼び出し側の誤りで、選択は INTERNAL を返す)。
var HostKeyTimings = []string{HostKeyTimingSurvey, HostKeyTimingEnroll, HostKeyTimingApply}

// --- Call.Args のキーと Secrets の形(spec 3.1.2 の引数の列。`--` を除いた名前) ---

// 引数のキー(Call.Args)。値の形は注記のとおり。
const (
	ArgHost                  = "host"      // --host: ssh://<user>@<host>:<port>(ポートの省略は 22、IPv6 は [...] で囲む)。要素 1 つ
	ArgAdminRef              = "admin-ref" // --admin-ref: 管理用資格情報の参照名。値は Call.Secrets[参照名] か Secrets.Get(参照名)。要素 1 つ
	ArgBackup                = "backup"    // --backup: latest | latest-any | <manifest_id>。要素 1 つ
	ArgTo                    = "to"        // --to: 空のディレクトリ。要素 1 つ
	ArgEndpoint              = "endpoint"  // --endpoint: 保管先の所在(spec 3.1.2.4 の (a))。要素 1 つ
	ArgBucket                = "bucket"    // --bucket: 同上
	ArgPrefix                = "prefix"    // --prefix: 同上
	ArgChecks                = "checks"    // --checks: all | none。要素 1 つ
	ArgReissue               = "reissue"   // --reissue: 値なし(キーがあれば真)
	ArgRestoreVerifyExpected = "expected"  // restore-verify: 期待する照合の入力(RestoreVerifyInput の JSON)。要素 1 つ
	ArgRestoreVerifyActual   = "actual"    // restore-verify: 復元先が報告した値(RestoreVerifyInput の JSON)。要素 1 つ
)

// 秘密の値の形(Secrets.Get の値と Call.Secrets の値。どちらも JSON)。
//   - Secrets.Get の値は全種別が {"kind", ...}(spec 4.5 の表)。ssh_key は SSHKeySecret(private_key・public_key・fp)、storage_keys は StorageKeysSecret({"keys":[…]})。
//   - SSHConn.Cred は SSHKeySecret.PrivateKey(OpenSSH 形式の秘密鍵の文字列)そのもの。
//   - Call.Secrets(--secrets-stdin)で渡す値は kind を持たず、管理用資格情報は OpenSSH 形式の秘密鍵の文字列、保管先の鍵は {"key_id":"…","secret":"…"}(spec 3.1.1「秘密の入力の規則」)。
const (
	SecretKindSSHKey      = "ssh_key"
	SecretKindStorageKeys = "storage_keys"
)

// SSHKeySecret は kind: ssh_key の秘密ファイルの値(sitate-pull、sitate-restore。spec 4.5 の表)。Fp は公開鍵の指紋。
type SSHKeySecret struct {
	Kind       string `json:"kind"` // SecretKindSSHKey
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	Fp         string `json:"fp"`
}

// StorageKeyEntry は storage_keys の keys[] の 1 要素(保管先準備アダプタの issue-keys の issued[] と同じ値。3.2.6)。
type StorageKeyEntry struct {
	Role   string `json:"role"` // write | read
	KeyID  string `json:"key_id"`
	Secret string `json:"secret"`
}

// StorageKeysSecret は kind: storage_keys の秘密ファイルの値(参照名は定義の storage[0].key_ref。spec 4.5 の表)。
type StorageKeysSecret struct {
	Kind string            `json:"kind"` // SecretKindStorageKeys
	Keys []StorageKeyEntry `json:"keys"`
}

// --- restore-verify の入力と結果(Restore.RestoreVerify。核に組み込みの照合: 復元したファイルの件数・大きさ・内容ハッシュをマニフェストと照合する) ---

// RestoreVerifyFile は照合する 1 ファイル(エントリ)。EntryRef はマニフェストのエントリの参照(f:<32 桁> か d:t<n>:<globals|custom>)。
type RestoreVerifyFile struct {
	EntryRef string `json:"entry_ref"`
	Size     int64  `json:"size"`
	Hash     string `json:"hash"` // blake3:<64 桁>
}

// RestoreVerifyInput は restore-verify の入力(Call.Args の expected と actual に JSON で渡す)。expected はマニフェストから、actual は復元先の報告。
type RestoreVerifyInput struct {
	Files []RestoreVerifyFile `json:"files"`
}

// restore-verify の mismatches[].reason。
const (
	RestoreVerifyMissing    = "missing"    // 期待にあって復元先に無い
	RestoreVerifyUnexpected = "unexpected" // 復元先にあって期待に無い
	RestoreVerifySize       = "size"       // 大きさが違う
	RestoreVerifyHash       = "hash"       // 大きさが同じで内容ハッシュが違う
)

// --- 復元先で核が流す台本の出力(spec 3.5 の表の 5・6。標準出力に JSON 1 つ) ---
// 復元テストで核が復元先に流す台本は順に (1) 隔離の検査 (2) 核とチェックの配置 で、続けて (3) `sitate restore ...`(Argv)を実行する。

// IsolationProbe は隔離の検査の 1 項目(Name は inbound・outbound・dns_name)。
type IsolationProbe struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
}

// DestProbeOut は (1) 隔離の検査の台本の標準出力。OS と Arch は復元先の `uname -s` と `uname -m` の値そのもの(配置の前に核が読み、配布物の
// `<os>-<arch>`(BundleOSArch)を決める。符号化の形は台本の側で決めてよく、核は台本の標準出力の JSON だけを読む)。
type DestProbeOut struct {
	Isolation []IsolationProbe `json:"isolation"`
	OS        string           `json:"os"`
	Arch      string           `json:"arch"`
}

// 配布物の `<os>-<arch>` の名前(spec 4.2。4.7 の core_sha256 の名前と同じ)。
const (
	BundleLinuxX86 = "linux-x86_64"
	BundleLinuxArm = "linux-arm64"
)

// BundleOSArch は復元先の uname の値(`uname -s`・`uname -m`)を配布物の `<os>-<arch>` に直す。x86_64・amd64 は linux-x86_64、aarch64・arm64 は linux-arm64。
// 対応しない組は ok が偽(復元テストは配置せず鍵を渡さず中止する)。
func BundleOSArch(unameS, unameM string) (name string, ok bool) {
	if unameS != "Linux" {
		return "", false
	}
	switch unameM {
	case "x86_64", "amd64":
		return BundleLinuxX86, true
	case "aarch64", "arm64":
		return BundleLinuxArm, true
	}
	return "", false
}

// PlacedFile は復元先で計算した 1 ファイルの SHA-256(sha256:<64 桁>)。Path は版ディレクトリからの相対パス。
type PlacedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// DestPlaceOut は (2) 配置の台本の標準出力。核はバックアップサーバー側の値と照らし、違えば鍵を渡さず中止する。
type DestPlaceOut struct {
	Files []PlacedFile `json:"files"`
}

// StoreAddr は引数で渡す保管先の所在(spec 3.1.2.4 の (a) の --endpoint・--bucket・--prefix)。
type StoreAddr struct {
	Endpoint, Bucket, Prefix string
}

// StoreOpener は引数の所在と読み取り鍵で ObjectStore を開く(提供: cli が store の実物を注入、利用: restore の (a))。
// 状態を持たない復元先で使う。secret は DestKeyLine の read_key.secret で、呼び出しの後に呼び出し側が消してよい。
type StoreOpener func(ctx context.Context, addr StoreAddr, keyID string, secret *Secret) (ObjectStore, error)

// DestKeyLine は (3) `sitate restore --snapshot-key-stdin` の標準入力の 1 行目(JSON)。2 行目以降は無く、標準入力は復元が終わるまで開けたままにする(終わりは取り消し)。
type DestKeyLine struct {
	SnapshotKey string `json:"snapshot_key"` // 標準の base64
	ReadKey     struct {
		KeyID  string `json:"key_id"`
		Secret string `json:"secret"`
	} `json:"read_key"`
}
