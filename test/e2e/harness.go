// Package e2e は結合試験のハーネス(make test-e2e。試験本体は WP U4 下書きが書く)。
// ハーネスが提供する操作の名前を骨格で固定する(作業指示書 5.1、GB-103)。中身は U4 下書きが書く。
package e2e

import "github.com/r2fish/sitate-backup/internal/contract"

// Harness は結合試験が使う操作。
type Harness interface {
	// SignApproval は承認の署名(承認鍵で change_id の記録に署名する)。
	SignApproval(changeID string) (signature []byte, err error)
	// ExtractPhrase は gate 経由の標準出力の先頭レコードからフレーズを取り出す(秘密の型。呼び出し側が消す)。
	ExtractPhrase(stdout []byte) (*contract.Secret, error)
	// Bootstrap は bootstrap を実行する。
	Bootstrap(args []string) (contract.Reply, error)
	// PhraseVerifyInput は phrase verify の標準入力に渡す入力を作る。
	PhraseVerifyInput(phrase *contract.Secret) ([]byte, error)
	// ReadWebhook は Webhook の受け手が記録した本文(1 件 1 行の JSON)を宛先のパスごとに読み出す。
	ReadWebhook(path string) ([][]byte, error)
}

// ErrNotImplemented は骨格の操作が返す誤り。
func ErrNotImplemented(op string) error { return contract.ErrNotImplemented("e2e." + op) }
