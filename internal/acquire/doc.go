// Package acquire は取得の流れと容器: 取得アダプタと容器の起動、ストリームの照合と分岐、索引(Index)、source enroll と source check の核の側、Surveyor。
//
// 仕様書の節: 7.5 の部品 acquire、3.2.2、3.3、3.4、4.3。
// 公開型は go doc で読む。不変条件はここに写さない。
package acquire
