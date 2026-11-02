package contract

import "time"

// 定数表(作業指示書 1 章。置き場 contract の行)と固定値の表。値は定数の名前で参照し、数値を直書きしない。
// 末尾の注は根拠の節。「*」は spec に名前が無く作業指示書で付けた名前。

const (
	// ChunkMin / ChunkAvg / ChunkMax はチャンク長(spec 4.3。M-03。PoC 4a-1 の暫定値)。切り方は形式の版に固定する。
	ChunkMin = 1 << 20
	ChunkAvg = 4 << 20
	ChunkMax = 16 << 20
	// AcquireErrorsMax は取得の結果の errors[]・warnings[] それぞれの上限(spec 3.2.2。設計の初期値)。
	AcquireErrorsMax = 50
	// ChunksPerGiB は 1 GiB あたりのチャンク数(spec 4.3、6.1.1。M-03。PoC 4a-1 の暫定値)。
	ChunksPerGiB = 224
	// TimeSkewMax は保管先の Date との許容差(spec 4.2。M-15。PoC 4a-1 の暫定値)。
	TimeSkewMax = 5 * time.Minute
	// SizeClassLargeGB は大きい元サーバーの閾値(spec 3.2.4、7.1。M-07・M-11。設計の初期値)。
	SizeClassLargeGB = 250
	// RunMaxDuration は実行の上限(spec 3.1.1 の規則 10。設計の初期値。試験ビルドは短くできる)。
	RunMaxDuration = 24 * time.Hour
	// RunAbortGrace は取り消し後の猶予(規則 10。試験ビルドは短くできる)。
	RunAbortGrace = 10 * time.Minute
	// RecordDeadline は中止の記録の締め切り(規則 21。試験ビルドは短くできる)。
	RecordDeadline = time.Minute
	// StateLockWait は state.lock の短い排他の待ち(規則 17、4.5。試験ビルドでは短くできる)。
	StateLockWait = 5 * time.Second
	// CheckTimeout は kind check の時限(spec 3.2.1、3.2.3、4.4)。
	CheckTimeout = 23 * time.Hour
	// CheckBufferBytes は取得時チェックの分岐のためる上限(spec 7.5)。
	CheckBufferBytes = 128 << 20
	// CheckExtHistMax / CheckFindingsMax はチェックの結果の上限(spec 3.2.3)。
	CheckExtHistMax    = 20
	CheckFindingsMax   = 100
	RansomTextMaxBytes = 64 << 10 // spec 3.2.7(ransom-note。イテレーション 2)

	// ENTROPY_*(spec 3.2.3 の params の 6 キーの大文字)。較正の起点であって確定値ではない(イテレーション 2 で監督が記入する)。
	// 暫定値。根拠は試験環境の合成データ(較正前)。
	EntropyMinFileBytes = 4096
	EntropySampleBytes  = 65536
	EntropyHighBits     = 7.9
	EntropyMinFiles     = 20
	EntropyRatioWarn    = 0.3
	EntropyRatioFail    = 0.6

	// kind ごとのアダプタの時限(spec 3.2.1。D-0111)。
	AdapterTimeoutAcquire     = 86400 * time.Second
	AdapterTimeoutRestoreDest = 1800 * time.Second
	AdapterTimeoutNotify      = 30 * time.Second
	AdapterTimeoutProvision   = 300 * time.Second

	// SurveyDuTimeoutS は元サーバーの調査の du の打ち切り(秒。spec 3.3。D-0122。暫定(PoC 前)。
	// 600 秒は推測: 数百 GB のディレクトリ 1 つの du が数分で終わるという見込みだけで、測っていない。監督が A2 の合成データで確かめて変える。
	// 試験ビルドと製品ビルドの両方で使い、空にしない。打ち切りの試験は単体試験の中だけで短い値を注入する。
	SurveyDuTimeoutS = 600

	RestoreScratchMethod = "gocryptfs" // spec 3.2.4

	NoncurrentSurgeThreshold = 100  // spec 4.2。M-16
	VerifySamplePerMonth     = 3000 // spec 4.2、6.1.1 の (3)
	SizeDriftRatio           = 0.05 // spec 4.2。M-23
	FormatVersion            = 1    // spec 4.3
	UndeliveredExpiry        = 168 * time.Hour
	SignedAtTolerance        = 10 * time.Minute
	NotifyRetryMax           = 3
	NotifyTimeout            = 30 * time.Second
	NotifyPendingExpiry      = 168 * time.Hour

	// DistURL は配布元の取得先(spec 4.7 の「製品」の欄。<version> を版に置き換える)。
	DistURL = "https://github.com/r2fish/sitate-backup/releases/download/<version>/"

	// 固定値の表。
	Argon2idMemoryKiB   = 256 * 1024
	Argon2idIterations  = 3
	Argon2idParallelism = 4
	// Argon2id の見出しの値の上限(spec 4.3)。測定値でなく製品が作らない値を拒否する線で、初期値を上げるときは同時に上げ、下げない。
	Argon2idMemoryMaxKiB   = 256 * 1024
	Argon2idIterationsMax  = 3
	Argon2idParallelismMax = 4
	// ManifestMax はマニフェストの展開後の上限(spec 4.3。参照 1 件を約 250 バイトとして約 6 万 7 千件の計算値)。
	ManifestMax            = 16 << 20
	NolockOpDeadline       = 60 * time.Second
	StateLockRetryInterval = 100 * time.Millisecond
	AdapterResultMax       = 1 << 20
	AdapterKillGrace       = 10 * time.Second
	AdapterStderrMax       = 64 << 10
	AdapterConnectTimeout  = 30 * time.Second
	AdapterRetryMax        = 3
	AdapterBackoffBase     = time.Second
	WebhookResponseMax     = 64 << 10
	ContainerMemoryMax     = 1 << 30
	StoreRetryMax          = 5
	StoreBackoffBase       = time.Second
	StoreRequestTimeout    = 300 * time.Second
	StoreParallelism       = 8
	ZstdLevel              = 3
	ProposalExpiry         = 168 * time.Hour
	ApprovedExpiry         = 720 * time.Hour
	NotifyBackoffBase      = 30 * time.Second

	// 保持(spec 4.2。日次 90 日・月次 12 か月の固定の定数)。
	RetentionDailyDays     = 90
	RetentionMonthlyMonths = 12
	// RetentionExtendPeriodDays は延長周期(spec 4.2)。月次のマニフェストの retain-until に足す。
	RetentionExtendPeriodDays = 7

	// StreamFragmentMax は sitate.stream/v1 の data 断片の最大長(形式の版 1。spec 3.2.2 が U0 に委ねた値)。
	// 1 MiB: 断片は本体をためずに流す単位で、チャンク長の最小(ChunkMin)と同じ桁。大きくするとメモリの先読みが増え、
	// 小さくするとフレームの見出しの割合が増える。設計上の判断で、測っていない。
	StreamFragmentMax = 1 << 20

	// FetchFragmentMax は sitate.fetch/v1 の断片の最大長(spec 3.1.4 が U0 に委ねた値)。読む側のメモリの上限で、
	// 超える断片は拒否する。値は StreamFragmentMax と同じ 1 MiB だが、形式が別なので定数も分ける。測っていない。
	FetchFragmentMax = 1 << 20

	// TestTickStartLocal は test-tick の模擬時間の開始日時(定義の timezone の現地時刻。月曜日、31 日の月の 1 日の 00:00)。
	TestTickStartLocal = TestTickStart
)

// 試験ビルドだけが読む環境変数の名前(作業指示書 3.3、5.1)。製品ビルドはこれらを読まない。
const (
	EnvTestRetentionDailyDays     = "SITATE_TEST_RETENTION_DAILY_DAYS"
	EnvTestRetentionMonthlyMonths = "SITATE_TEST_RETENTION_MONTHLY_MONTHS"
	EnvTestRunMaxDurationS        = "SITATE_TEST_RUN_MAX_DURATION_S"
	EnvTestRunAbortGraceS         = "SITATE_TEST_RUN_ABORT_GRACE_S"
	EnvTestRecordDeadlineS        = "SITATE_TEST_RECORD_DEADLINE_S"
	EnvTestStateLockWaitS         = "SITATE_TEST_STATE_LOCK_WAIT_S"
	// EnvExtraCAFile は Webhook の受け手の追加の CA 証明書のファイル(試験ビルドの核がアダプタの起動時に置く変数。製品ビルドは渡さない)。
	EnvExtraCAFile = "SITATE_EXTRA_CA_FILE"
	// EnvTestTrustPubKeyFile は試験ビルドの信頼の起点を差し替える使い捨ての公開鍵(minisign の .pub)の置き場。
	EnvTestTrustPubKeyFile = "SITATE_TEST_TRUST_PUBKEY_FILE"
)

// 試験用の保管先の接続先と資格情報を試験へ渡す環境変数の名前(test/env/bin/test-env.sh が .run/<NAME>/env に書く)。
const (
	EnvTestS3Kind      = "SITATE_TEST_S3_KIND"
	EnvTestS3Endpoint  = "SITATE_TEST_S3_ENDPOINT"
	EnvTestS3Region    = "SITATE_TEST_S3_REGION"
	EnvTestS3AccessKey = "SITATE_TEST_S3_ACCESS_KEY_ID"
	EnvTestS3SecretKey = "SITATE_TEST_S3_SECRET_ACCESS_KEY"
	EnvTestS3Bucket    = "SITATE_TEST_S3_BUCKET"
	// 書き込み鍵・読み取り鍵(KIND=fake のときだけ作る。MinIO は管理用鍵だけ)。
	EnvTestS3WriteAccessKey = "SITATE_TEST_S3_WRITE_ACCESS_KEY_ID"
	EnvTestS3WriteSecretKey = "SITATE_TEST_S3_WRITE_SECRET_ACCESS_KEY"
	EnvTestS3ReadAccessKey  = "SITATE_TEST_S3_READ_ACCESS_KEY_ID"
	EnvTestS3ReadSecretKey  = "SITATE_TEST_S3_READ_SECRET_ACCESS_KEY"
	EnvTestS3BucketPrefix   = "SITATE_TEST_S3_BUCKET_PREFIX"
	EnvTestPGHost           = "SITATE_TEST_PG_HOST"
	EnvTestPGPort           = "SITATE_TEST_PG_PORT"
	EnvTestPGUser           = "SITATE_TEST_PG_USER"
	EnvTestPGPassword       = "SITATE_TEST_PG_PASSWORD"
)

// TestBuildTag は試験ビルドのタグの名前(go test -tags でも使う)。
const TestBuildTag = "sitate_testbuild"
