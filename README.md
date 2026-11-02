# Sitate-backup

## 審査員に見てもらう道

(文面は監督が期間中に決める。D-0149、D-0150)

## このリポジトリの中身について

(文面は設計リポジトリの監督が書く。D-0149)

## 外部の公開情報から作ったデータ

(`data/prices/src/` とその出所。文面は設計リポジトリの監督が書く。D-0149、D-0153)

## 開発者向け

- 試験: `make test`(Linux の開発用コンテナ)、`make test-app`(作業端末)、失敗の内訳は `make contract-status`。
- ビルド: `make build`(製品)、`make build-test`(試験)。出力は `dist/<product|test>/bundle/<版>/`(版は `VERSION=`、既定 `0.1.0-dev`)。
  `<os>-<arch>/sitate`、`<os>-<arch>/adapters/<kind>/<name>/`、ソースのアーカイブ `sitate-<版>-src.tar.gz`、SBOM `sitate-<版>.spdx.json`(SPDX 2.3)。
  最初の commit の前はソースのアーカイブを飛ばす(警告)。
- 署名: `make sign-test`(使い捨ての鍵 `test/env/.run/keys/`)。配布用の `make sign`・`make sign-installer` は利用者だけが自分の端末で実行する。
- 試験用の保管先: `make test-env-up NAME=<WP> [KIND=fake]`、`make test-env-down NAME=<WP>`。接続先と資格情報は `test/env/.run/<NAME>/env`(git 管理外)。
