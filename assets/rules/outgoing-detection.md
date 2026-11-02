# チャット欄の送信前の検出の規則

チャット欄の入力は、エージェントに渡す前にアプリの主プロセスが次の規則で調べる。1 つでも該当したら、送信せず、入力欄を空にして主プロセスのメモリから消す。画面の会話の表示、エージェントのプロセス、ログのどこにも置かない。

- 該当した種類だけをカタログの固定文で示す。入力の一部は示さない。
- 秘密の入力ボックスへの誘導と、失効・再発行の案内を出す。
- 誤検出でも、送る操作の上書きは置かない。人は該当した部分を除いて書き直す。

## 規則(アプリ向け)

下の JSON が機械が読む本体。`pattern` は正規表現(ECMAScript)。`kind` はカタログのキー `chat.secret.detected.<kind>` に対応する。

```json
{
  "rules": [
    {"kind": "keychain_value", "type": "substring_of_keychain_values", "min_length": 8},
    {"kind": "phrase", "type": "wordlist_run", "wordlist": "bip39-english", "min_run": 12, "separators": "空白・改行・読点・番号"},
    {"kind": "private_key", "type": "regex", "pattern": "-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----"},
    {"kind": "llm_api_key", "type": "regex", "pattern": "sk-ant-[A-Za-z0-9_-]{20,}"},
    {"kind": "storage_key", "type": "regex", "pattern": "(AKIA|ASIA)[A-Z0-9]{16}"},
    {"kind": "storage_key", "type": "regex", "pattern": "(?<![A-Za-z0-9+/])K[0-9]{3}[A-Za-z0-9+/]{27}(?![A-Za-z0-9+/])"},
    {"kind": "webhook_url", "type": "url_with_long_token", "scheme": "https", "min_token_length": 20, "token_chars": "英数字・下線・ハイフン", "where": "パスかクエリ"}
  ]
}
```

## 補足

- キーチェーンにある値は、8 文字以上のものを部分文字列として含むかで調べる。値そのものはこの規則ファイルに書かない。
- リカバリーフレーズは、BIP-39 の英単語表の語だけが 12 語以上続くかで調べる。
- B2 のアプリケーションキーは、語の区切りで挟まれた形だけを該当とする。
