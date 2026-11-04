# U1g 完了条件 → 試験の対応表(作業指示書 5.7。テスト仕様書 6.1 の 1)

作成: 2026-11-04(期間中)。作業ツリー wp/maps(開始 commit f503da6)。試験は変えていない。
実行: `make shell-linux CMD='go test -count=1 -v ./internal/... ./adapters/... ./data/... ./cmd/... ./tools/...'`(既定のタグと `-tags sitate_testbuild`)。`internal/keys` は `ok`、FAIL 0。ログは作業の scratchpad の `maps-go-default.log`・`maps-go-tb.log`。
作業指示書 5.7 の完了条件は番号の無い 1 つの長い文なので、項目に a〜w の記号を付けた。試験名は関数名で、場所は `internal/keys/` の `*_test.go`(他のパッケージは明記)。SKIP は緑に数えない。

| 項目 | 試験 | 結果 | 備考 |
|---|---|---|---|
| 共通(契約。鍵表・キーリング・製品の印・チャンク鍵とスナップショット鍵の包み・信頼の起点の表のベクトル) | `contract_keys_test.go`(`TestContractKeyServiceDerivedKey`/`ChunkID`/`ChunkKeyWrap`/`SnapshotKey`/`OpenKeyring`/`IssueKeys*`、`TestContractVerifierVerify`、`TestContractKeysInit`、`TestContractKeysKit`)、`contract_keys_phrase_test.go`、`contract_keys_cmds_test.go`、`contract_keys_doctor_test.go`、`contract_keys_bucket_test.go`、`contract_keys_reissue_test.go`、`contract_keys_keyref_test.go`、`contract_keys_secrets_test.go`、`contract_nildeps_test.go` | 一部 | `TestContractKeysStorageIssueReadKey`・`TestContractKeysStorageRevokeReadKey`・`TestContractKeysSecretsSet`・`TestHardenMemoryLocksProcess` は SKIP(`TestContractKeysSecretsSet` は条件の `secrets set` の項目 o に当たる契約で、SKIP の理由を読んでいない。要確認) |
| a `init` が `--secret-fd` 無しで `SECRET_FD_REQUIRED` | `contract_keys_test.go` の 375 行付近(`harness.ExpectCode(... CodeSecretFdRequired)`。どの題の中かは未確認) | 緑 | |
| b `phrase verify` の確定までキーリングを保管先へ書かない | `TestKeysPhraseConfirmedNotSetWhenCommitFails`、`TestKeysInitWritesNothingOutsidePending`、`TestContractKeysPhraseVerify`、`TestKeysPhraseVerifyResumesAfterPartialWrite` | 緑 | |
| c 確定後の `init` が `ALREADY_INITIALIZED` | `TestKeysInitRerunAndAlreadyInitialized`(`keys_extra`)、`keys_review_test.go:225`、`TestKeysInitAfterSealRejected` | 緑 | |
| d `kit` が `--format json` だけを受ける | `contract_keys_test.go` の 380 行付近(`pdf`・`printable` を拒否)、`TestContractKeysKit`、`TestKeysKitSuccess` | 緑 | |
| e `kit` の結果が `kit_version` と `fields`(フレーズを含まない) | `TestKitFieldsSteps`(`kitfields`)、`TestContractKeysKit`、`TestKeysInitModesAndNoWordsArray` | 緑 | |
| f `bucket check` の `permissions/<i>/actual` と `expected` の一致 | `TestContractKeysBucketCheck`、`TestKeysBucketMismatchAllRoutes`、`TestContractKeysBucketCheckTooPowerful`、`TestContractKeysBucketCheckUntested` | 緑 | 試験用の MinIO に対する同じ判定は U1f-2 の結合の後(別の記録) |
| g `KEY_ALLOWS_UNVERSIONED_DELETE`(警告、終了 0) | `TestContractKeysBucketCheckUnversionedDelete`、`contract_keys_bucket_test.go`(155・176 行付近の許す・許さない両方) | 緑 | |
| h `doctor`(段階 defined)の時刻のずれと、オフセットを与えたときずれと判定しない | `TestContractKeysDoctorTimeSkew`(`contract_keys_cmds`) | 緑 | オフセットを与えた時計の偽物でずれと判定しない部分の専用の題名は特定できなかった(本文を読んでいない) |
| i 署名の合わないアダプタ・価格表を使わない | `internal/adapter/contract_adapter_test.go`(74・98・101 行付近)、`internal/adapter/trust_run_test.go`、`internal/cli/prices_signed_test.go: TestSignedPricesBadOrMissingSignatureMeansNoTable` | 緑 | 使い捨ての鍵を試験用の信頼の起点に差し込む形は本文を読んでいない |
| j `check_images` が `ok: true`・`count: 0`、他の要素は `count` を持たない | `internal/cli/osprobe_test.go: TestProbeCheckImagesAndUnknown`、`internal/contract/contract_enums_test.go`(`DoctorCheckImagesCount` が 0)、`TestContractKeysDoctorOSChecks` | 一部 | 「他の要素が `count` を持たない」を確かめる題は特定できなかった |
| k `doctor` bootstrap 段階が NFS・CIFS・FUSE で `LOCK_UNAVAILABLE` | `TestContractKeysDoctorStateFS`(`contract_keys_doctor`) | 緑 | `statfs` の差し替えか否かは本文を読んでいない |
| l `seal` の前は候補を読み、候補が無ければ `DEF_INVALID` | `TestKeysInitNeedsCandidateDefinition`、`TestBucketCheckWithoutKeyRefUsesCandidateStorageKeyRef`、`TestBucketCheckWithoutKeyRefAndWithoutCandidateIsDefInvalid`、`TestContractKeysInitStorageNoCandidate` | 緑 | |
| m Argon2id のパラメータを定数の名前で参照し、キーリングに記録 | `internal/format/contract_format_test.go: TestContractCryptoArgon2id`、`internal/format/format_test.go: TestArgon2idParamsMatter`、`TestContractKeysPhraseVerifyKeyringVersion` | 一部 | 「定数の名前で参照する(数値を直書きしない)」は `make lint-size` ではなく `internal/contract` の定数の検査の範囲。この表では確かめていない |
| n フレーズは BIP-39 で 24 語 | `TestKeysInitModesAndNoWordsArray`、`internal/restore/contract_restore_test` 側の語数の題は特定できなかった | 一部 | BIP-39 の語彙表そのものを確かめる題は特定できなかった(24 語の長さの確認までを `contract_keys_test.go` が持つかは未読) |
| o `secrets set <key_ref> --role write\|read --stdin` | `TestContractKeysSecretsSet`(SKIP)、`TestContractKeysInitStorageSecrets`、`TestContractKeysInitStorageSecretsNotInErrors` | 試験なし(実質) | 直接の題 `TestContractKeysSecretsSet` が SKIP。skip を緑に数えない。他の 2 題は `init-storage` の秘密の扱いで `secrets set` ではない |
| p 公開鍵が 2 つのとき鍵 ID が一致する鍵で検証し、無ければ使わない | `TestVerifierMultipleAnchors`(`keys_extra`) | 緑 | |
| q `phrase verify` の確定で `bin/` に核・アダプタ・署名を spec 4.2 の形で書く | `TestKeysPhraseVerifyBundleRejects`、`TestKeysPhraseVerifyRealLayout`、`internal/store/review2_test.go: TestExtendBKitBinAndLatestConfig` | 緑 | 署名が合わないとき書かずに失敗することは `...BundleRejects` |
| r 孤児の鍵 | `TestContractKeyServiceIssueKeysOrphaned`、`TestContractKeysInitStorageOrphan`、`TestContractKeysInitStorageOrphanedFromIssue`、`TestKeysOrphanRecordedWhenPutAndRevokeFail`、`TestKeysOrphanedKeyIDsFromAdapterError`、`TestKeysLedgerMergeIsOrderIndependent` | 緑 | |
| s 発行後の `check` の失敗で `KEY_RECONCILE_UNVERIFIED` | `TestContractKeysInitStorageReconcileFails`、`contract_keys_bucket_test.go`(317 行付近) | 緑 | |
| t `init-storage` の再実行・台帳が空の初回でも照合が走る | `TestContractKeysInitStorageReissue`、`TestContractKeysInitStorageReissueCheckFails`、`TestContractKeysInitStorageAlreadyInitialized` | 一部 | 「台帳が空の初回でも照合」の題は未特定 |
| u 記録の無い新しいホストの `storage issue-read-key` は照合しない | `TestContractKeysStorageIssueReadKey`(SKIP) | 未実行 | SKIP(理由は読んでいない)。他に該当する題を特定できなかった |
| v `init --adopt` が仮登録した定義を `Index` の再構成に渡す | — | 試験なし | `adopt` を含む試験が `internal/keys`・`internal/acquire`・`internal/cli` に見当たらない(`grep -i adopt` が 0 件) |
| w 追加(D-0159/0162/0163 などで加わった `init` の周り) | `keys_mpharden_test.go`(`TestPhraseVerifyRejectsPlantedPendingAfterConfirm` ほか)、`journal_master_test.go`(`TestJournalKeepsIntentBeforeAnyMaster` ほか 5)、`init_timers_order_test.go`、`memguard_linux_test.go`(`TestHardenMemoryFailureWarnsAndKeepsGoing`)、`bucket_keyref_default_test.go`、`issue_adapter_test.go` | 緑 | `TestHardenMemoryLocksProcess` は SKIP(`ulimit` が要る) |

## 集計(行ごと 24 行)
- 緑 16 / 一部 5(共通・j・m・n・t) / 未実行 1(u) / 試験なし 2(o 実質・v) / 範囲外 0。
- 「試験なし」: v(`init --adopt` が渡す定義)。o は直接の題が SKIP(`TestContractKeysSecretsSet`)で、緑の根拠にならない。
- 本文を読まずに題名だけで対応づけた行(h・i・k・m)は、受け入れの前に題の本文の確認が要る。
