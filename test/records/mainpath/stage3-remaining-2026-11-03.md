# 実物の ssh-host に対する restore-test の remaining の記録(2026-11-03)

- 対象: 仕様書 1.2.8(D-0159)を反映した作業ツリー(WP `mp-restore`)。台本は `test/mainpath/stage2_*_test.go` と `stage3_remaining_test.go`(第 2 段の経路の最後に確かめを足した)。
- 実行: `bash test/env/bin/accept-env.sh up mprs`、`LOCK_DEFAULT_DAYS=90 make test-env-up NAME=mprs` の後に `make test-mainpath STAGE=2 NAME=mprs`。終わったら `make test-env-down NAME=mprs` と `accept-env.sh down mprs`。
- 定義は `restore.ssh_from` を持たない(`s2.definition()` の `ssh_from` の行を外した)。

## 結果(逐語)

```
stage3_remaining_test.go:28: restore_dest_enrollment: observed_from=172.20.0.5 egress_allow=[172.19.0.10]
stage3_remaining_test.go:47: restore-test の teardown (逐語): {"done":true,"remaining":[],"verified_by":"ssh"}
stage3_remaining_test.go:48: restore-test の teardown.done=true remaining=[]
--- PASS: TestStage2MainPath (46.37s)
```

- `observed_from` は、台本が `ip route get` で調べた「dest から見たバックアップサーバーのアドレス」と一致した(台本が突き合わせて、違えば失敗にする)。アダプタは `allow_ssh_from` を省いた要求で、管理用アカウント(root でなく、実物の sudo が環境を消す)の接続の `SSH_CONNECTION` の先頭を sudo に入る前に読んで受信許可にした。`restore-dest enroll`・`restore-dest check`(隔離の 3 項目 ok)・`restore-test`(`deferred` が偽、直近の世代を復元して照合)がこの定義で通った。
- 実物の ssh-host に対する 1 回の `restore-test` の `teardown.remaining` は `[]`、`done` は真、`verified_by` は `ssh`。stop が除く専用ユーザーの常駐の処理(`systemd --user`、`(sd-pam)`、D-Bus、podman の pause)のほか、この復元で残った処理は無かった。
- アダプタ単体の受入の試験(同じ環境の dest、実物の sudo): `TestAcceptEnrollObservedFromAndHostKeyPin` が合格(`allow_ssh_from` を省いた登録で `observed_from` が試験の送信元と一致し、`/etc/sitate/isolation.nft` に入る。違う `host_key_fp` では `hostkey_mismatch`・`seen_fp` が環境の記録と一致し何も変えない。省いた初回の登録は初回接続のホスト鍵を返す)。同じ実行で `adapters/restore-dest/ssh-host` の受入の試験は全件合格(`ok  	github.com/r2fish/sitate-backup/adapters/restore-dest/ssh-host	110.923s`)。

## 未確認

- 登録の後の dest は受信をバックアップサーバーだけに絞る(隔離)ので、台本の端末から restore-test の後の専用ユーザーの処理の一覧(`ps`)を取れず、除かれた常駐の処理の中身は直接は見ていない。`remaining` が空であることだけを確かめた。
- `remaining` に `process` が載る実機の場面(復元の途中で処理が残る)は、アダプタ単体の `TestAcceptStopRemaining`(別の SSH の接続上の `sleep` が残る)で載ることを確かめた範囲に限る。復元の失敗の経路で実物に処理が残る場面は未検証。
- NAT の内側(`SSH_CONNECTION` の先頭が実際のバックアップサーバーのアドレスと違う場面)は受入試験の環境に無く、未検証(その場合は `restore.ssh_from` に書く。仕様書 3.2.4)。
