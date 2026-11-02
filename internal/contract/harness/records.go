package harness

import (
	"encoding/json"
	"errors"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// SecretRecordSchema は標準出力の先頭レコードの schema(spec 3.1.4)。
const SecretRecordSchema = "sitate.secret-string/v1"

// ClassifyStdout は標準出力のレコード列(1 行 1 レコード)を spec 3.1.4 の規則で分ける。
// 戻り値の code は破棄すべきとき 3.7 のアプリの原因コード(ENVELOPE_SECRET_MISMATCH / ENVELOPE_TOO_MANY_RECORDS / ENVELOPE_INVALID)、
// 受理できるときは空。hasSecret は先頭が秘密のレコードかどうか。
func ClassifyStdout(records []json.RawMessage) (code string, hasSecret bool, err error) {
	if len(records) == 0 {
		return "ENVELOPE_INVALID", false, errors.New("レコードが無い")
	}
	if len(records) > 2 {
		return "ENVELOPE_TOO_MANY_RECORDS", false, nil
	}
	var head struct {
		Schema string `json:"schema"`
	}
	_ = json.Unmarshal(records[0], &head)
	hasSecret = head.Schema == SecretRecordSchema
	if hasSecret && len(records) == 1 {
		return "ENVELOPE_SECRET_MISMATCH", true, nil // 秘密のレコードだけで封筒が無い
	}
	if len(records) == 2 && !hasSecret {
		return "ENVELOPE_INVALID", false, nil // 先頭が秘密のレコードでも封筒でもない
	}
	if hasSecret && contract.Validate("cli/secret-record.json", records[0]) != nil {
		return "ENVELOPE_SECRET_MISMATCH", true, nil // 秘密のレコードの形が不正(23 語・余分な欄・keyring_version 0 など)
	}
	envRec := records[len(records)-1]
	var env struct {
		Schema        string `json:"schema"`
		PhraseEmitted bool   `json:"phrase_emitted"`
	}
	if json.Unmarshal(envRec, &env) != nil || env.Schema != "sitate.out/v1" {
		if hasSecret {
			return "ENVELOPE_SECRET_MISMATCH", true, nil // 秘密が見えていて、次が封筒でない([秘密, 秘密]・[秘密, 別の schema]・[秘密, null])。秘密だけの出力と同じ
		}
		return "ENVELOPE_INVALID", false, nil
	}
	if env.PhraseEmitted != hasSecret {
		return "ENVELOPE_SECRET_MISMATCH", hasSecret, nil
	}
	return "", hasSecret, nil
}

// ClassifyStream は行(1 行 1 レコード。最後の行は途中で切れていてもよい)と、出力の終わり方 end("complete" か "ssh_disconnected")で分ける。
//   - ssh_disconnected: 秘密が見えていても見えていなくても SSH_DISCONNECTED(破棄。秘密は渡さない)。
//   - complete で JSON として読めない行がある(封筒が途中で切れた): 秘密が先頭に見えていれば ENVELOPE_SECRET_MISMATCH、見えていなければ ENVELOPE_INVALID。
//   - それ以外は ClassifyStdout と同じ。
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
	if len(records) > 0 {
		var head struct {
			Schema string `json:"schema"`
		}
		_ = json.Unmarshal(records[0], &head)
		hasSecret = head.Schema == SecretRecordSchema
	}
	switch {
	case end == "ssh_disconnected":
		return "SSH_DISCONNECTED", hasSecret, nil
	case broken && hasSecret:
		return "ENVELOPE_SECRET_MISMATCH", true, nil
	case broken:
		return "ENVELOPE_INVALID", false, nil
	}
	return ClassifyStdout(records)
}
