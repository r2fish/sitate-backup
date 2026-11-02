// Package adapter はアダプタ共通の起動: run、adapter.json の読み取りと検証、要求と結果の JSON、kind ごとの時限、標準エラーの制限、署名検証の呼び出し。
//
// 仕様書の節: 7.5 の部品 adapter、3.2.1。
// 公開型は go doc で読む。不変条件はここに写さない。
package adapter
