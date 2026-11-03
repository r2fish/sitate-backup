# 試験用ドライバの偽の LLM の口での通しの記録

`make test-app-e2e NAME=<名前>`(`test/wiring/e2e.sh`)の記録のうち、A2(手順 1〜13)と A6(注入文)の 1 回分。LLM API キー・B2 は使っていない(LLM の代役と試験用の MinIO)。
`llm_request` の本文は大きさだけを残して省いた(全文は実行のたびに `--out` へ出る)。人の役の値は偽の値で、記録に出ていないことは各記録の `secret_check` にある。
