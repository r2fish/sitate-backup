// Package store は保管先 I/O と保持: S3 の必要な部分、verify、extend、prune、保持区分、hold(ObjectStore、Retention、StoreProbe)。
//
// 仕様書の節: 7.5 の部品 store、4.2。
// 公開型は go doc で読む。不変条件はここに写さない。
package store
