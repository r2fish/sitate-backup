package contract

// 元サーバーの調査(spec 3.3)と定義ファイルの source 節(spec 4.1)の固定の値。判定の実装は U1c(Surveyor)と U1d(def validate)で、
// ここは名前・形式・一覧だけを持つ。判定の試験ベクトルは testdata/survey/、読み方は harness/survey.go。

// 調査の出力の形式の名前と範囲(スキーマは schema/receiver/survey.json)。
const (
	SourceSurveyFormat = "sitate.source-survey/v1"
	RecvSurveyFormat   = "sitate.recv-survey/v1"
	SurveyScopeFull    = "full"
	SurveyScopeDBOnly  = "db_only"
)

// 核が調査の処理と source-setup の台本の先頭に置く代入の変数名(試験ベクトル testdata/survey/embed.json)。
// 値は単一引用符で囲み、値の中の単一引用符は、引用を閉じて、バックスラッシュ付きの引用符を置き、引用を開き直す形に置き換える。改行・NUL・(クラスタの行では)タブを含む値は埋め込めない。
const (
	EmbedVarScope     = "SITATE_SCOPE"
	EmbedVarDuTimeout = "SITATE_DU_TIMEOUT_S"
	// EmbedVarClusters は信じるクラスタの一覧。1 クラスタ 1 行で、行は socket_dir・port・major・raw_path... をタブで区切ったもの(source-setup だけ)。
	EmbedVarClusters = "SITATE_CLUSTERS"
)

// 候補の規則(spec 3.3)。
const (
	// PGMajorMin は v1 が対応する PostgreSQL の主版の下限(13 未満は対象外の版)。
	PGMajorMin = 13

	MountClassCandidate   = "candidate"
	MountClassParent      = "parent"
	MountClassDBData      = "db_data"
	MountClassUnsupported = "unsupported"

	DirLabelHome    = "home"
	DirLabelSrv     = "srv"
	DirLabelDBMount = "db_mount"

	FsTypeOther = "other"
)

// SurveyDirParents は source survey が直下のディレクトリを調べる 2 か所(label は home / srv)。
var SurveyDirParents = []string{"/home", "/srv"}

// PGSocketDirs はクラスタのソケットを探す 3 か所。先頭 2 つは root の作るディレクトリ。
var PGSocketDirs = []string{"/run/postgresql", "/var/run/postgresql", "/tmp"}

// ファイルシステムの種類の一覧(spec 3.3「候補の規則」)。この一覧に無い名前は出力で other に置き換える。
// 「など」で開いている uncounted は、列挙された名前だけを持つ(ほかの擬似ファイルシステムは根に置けない場所の下にある)。
var (
	FsTypesSupported   = []string{"ext2", "ext3", "ext4", "xfs", "btrfs"}
	FsTypesUncounted   = []string{"proc", "sysfs", "devtmpfs", "devpts", "tmpfs", "overlay", "squashfs", "cgroup2"}
	FsTypesUnsupported = []string{"nfs", "nfs4", "cifs", "vfat", "exfat", "ntfs"}
)

// UncoveredKinds は source check の uncovered[] の kind。ActionableKinds は足せる候補(actionable[])。
var (
	UncoveredKinds  = []string{"mount", "mount_parent", "db", "db_cluster", "large_object", "mount_unsupported", "db_unsupported"}
	ActionableKinds = []string{"mount", "mount_parent", "db", "db_cluster"}
)

// 定義ファイルの source.declined(spec 4.1)。要素は除外 id と、種別をキーにした 1 項目。
const (
	DeclinedKeyMount   = "mount"
	DeclinedKeyCluster = "postgresql_cluster"
	// DeclinedIDPattern は除外 id(対象 id と同じ規則で接頭辞が d)。
	DeclinedIDPattern = `^d[1-9][0-9]{0,3}$`
)

// 根に置けない場所(spec 4.1。D-0122 の (e))。files の根が (i)(ii)(iii) に当たれば DEF_INVALID(reason: root_forbidden)。
// (iv)(DB の生のファイルの場所に触れる根)は元サーバーでなければ分からないため、source enroll の段 roots が検査する。
const RootForbiddenI = "/"

// RootForbiddenII は (ii): そのものだけが置けない(下の階層は置ける)。
var RootForbiddenII = []string{"/etc", "/var", "/home", "/srv", "/opt"}

// RootForbiddenIII は (iii): そのものとその下が置けない。
var RootForbiddenIII = []string{"/proc", "/sys", "/dev", "/run", "/boot", "/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/etc/ssh", "/root", "/tmp", "/var/tmp"}

// 調査の処理(Surveyor が流す固定の処理)の終了状態と、核の読み替え(spec 3.1.3 の SOURCE_SURVEY_FAILED。判定の場所は下のとおり)。
//   - OS が Linux でない: 調査の処理が最初に uname -s を見て、何も出さず SurveyExitUnsupportedOS で終わる(判定は調査の処理)。核は step: prereq・reason: unsupported_os に読み替える。
//   - sudo -n が使えない: sudo は調査の処理を起動できず、標準出力に何も出さずに終わる(sudo が無ければ終了 127、パスワードを求めれば 1)。
//     核は User が root でなく、標準出力が空で、終了状態が 1 か 127 のときだけ step: sudo・reason: sudo_unavailable に読み替える(判定は核)。
const SurveyExitUnsupportedOS = 77

// SurveyFailPairs は SOURCE_SURVEY_FAILED の detail の (step, reason) の許される組。
var SurveyFailPairs = [][2]string{{"connect", "auth"}, {"connect", "key_unparsable"}, {"sudo", "sudo_unavailable"}, {"prereq", "unsupported_os"}}
