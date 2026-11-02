package harness

import (
	"encoding/json"
	"errors"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// SecretRecordSchema は標準出力の先頭レコードの schema(spec 3.1.4)。
const SecretRecordSchema = "sitate.secret-string/v1"

// ClassifyStdout は標準出力のレコード列(1 行 1 レコード)を spec 3.1.4 の判定の順で分ける。
// 戻り値の code は破棄すべきとき 3.7 のアプリの原因コード、受理できるときは空。hasSecret はどれかのレコードが秘密のレコードかどうか。
func ClassifyStdout(records []json.RawMessage) (code string, hasSecret bool, err error) {
	if len(records) == 0 {
		return "ENVELOPE_INVALID", false, errors.New("レコードが無い")
	}
	hasSecret = anySecret(records)
	if len(records) > 2 {
		return "ENVELOPE_TOO_MANY_RECORDS", hasSecret, nil
	}
	if !hasSecret {
		if anyEmitted(records) {
			return "ENVELOPE_SECRET_MISMATCH", false, nil
		}
		if len(records) != 1 || !isEnvelope(records[0]) {
			return "ENVELOPE_INVALID", false, nil // [封筒, 別の schema] を含む
		}
		return "", false, nil
	}
	if len(records) == 2 && schemaOf(records[0]) == SecretRecordSchema && contract.Validate("cli/secret-record.json", records[0]) == nil &&
		isEnvelope(records[1]) && anyEmitted(records[1:]) {
		return "", true, nil
	}
	return "ENVELOPE_SECRET_MISMATCH", true, nil
}

func schemaOf(r json.RawMessage) string {
	var head struct {
		Schema string `json:"schema"`
	}
	_ = json.Unmarshal(r, &head)
	return head.Schema
}

func isEnvelope(r json.RawMessage) bool { return schemaOf(r) == "sitate.out/v1" }

func anySecret(records []json.RawMessage) bool {
	for _, r := range records {
		if schemaOf(r) == SecretRecordSchema {
			return true
		}
	}
	return false
}

func anyEmitted(records []json.RawMessage) bool {
	for _, r := range records {
		var env struct {
			PhraseEmitted bool `json:"phrase_emitted"`
		}
		if isEnvelope(r) && json.Unmarshal(r, &env) == nil && env.PhraseEmitted {
			return true
		}
	}
	return false
}

// ClassifyStream は行(1 行 1 レコード。最後の行は途中で切れていてもよい)と、出力の終わり方 end("complete" か "ssh_disconnected")で分ける。
// 順は ClassifyStdout と同じで、先頭に SSH の切断を置く。
func ClassifyStream(lines []string, end string) (code string, hasSecret bool, err error) {
	var records []json.RawMessage
	broken := false
	for _, l := range lines {
		if !json.Valid([]byte(l)) {
			broken = true
			continue
		}
		records = append(records, json.RawMessage(l))
	}
	hasSecret = anySecret(records)
	switch {
	case end == "ssh_disconnected":
		return "SSH_DISCONNECTED", hasSecret, nil
	case len(lines) > 2:
		return "ENVELOPE_TOO_MANY_RECORDS", hasSecret, nil
	case broken && (hasSecret || anyEmitted(records)):
		return "ENVELOPE_SECRET_MISMATCH", hasSecret, nil
	case broken:
		return "ENVELOPE_INVALID", false, nil
	}
	return ClassifyStdout(records)
}
