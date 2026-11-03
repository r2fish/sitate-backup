# mp-core3 の受け入れ(2026-11-03)

- 範囲: 署名を要さない `attest` の 3 段を `init` の前に記録する、`seal` の承認で `notify.proof_confirmed` を立てる、`tools/sign.sh` の指紋を仕様の形にする、結線の試験で手順 1〜13 の合格を要求する。
- 再実行(新しい環境): `make build-test sign-test build-app-test` は終了 0。`make test-app-real`: 合格した手順 1,2,3,4,5,8,9,6,7,10,13,11,12、合格しなかった手順なし、pass 1、fail 0。`npm --prefix app test`: pass 261、fail 0。`make lint-size` は合格。
- 監督の修正: 担当の試験 `TestAttestUnsignedStepsRecordBeforeInit` は `sitate_testbuild` で赤だった(模擬時間の変数が無い)。試験の準備を直し、両タグと `-race` で ok を確かめた。
- 監督の変異は `git` の記録のとおり全件赤(署名を要さない段も `init` を要する、全部の段を `init` の前に通す、確認語の一致を記録しない)。
- 手順 13 の合格は空虚: `TIMER_NAMES` が空で `schedule` の中身を見ていない。裁定を依頼中(申し送りの 39 節)。
- `SIGNATURE_INVALID` の `detail.reason` は送る欄の表に無く、モデルに届かない。同じ節で依頼中。
- 別起動のレビューは未実施。`tools/fingerprint.sh` はどこからも使われていない。
