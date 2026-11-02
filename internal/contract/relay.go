package contract

// 中継の形式 sitate.relay/v1(spec 3.1.2.4 の (b)、7.4)。スナップショット鍵の行(--snapshot-key-stdin)に続けて標準入力へ流す。
// 1 行目 start、続いて object(見出し 1 行と、保管先から読んだままの暗号文 size バイト)の繰り返し、最後に end の 1 行。
// end が無いまま標準入力が閉じたときは取り消し(読み出しを止めて終わる。spec 3.5)。スキーマは schema/data/relay.json。
const RelayFormat = "sitate.relay/v1"

// RelayObject は中継する 1 オブジェクトの見出し。Kind は manifest か chunk(エントリ・鍵表・データのチャンクはどれも chunk)。
// Key はプレフィクスを除いた保管先のキー(manifests/<manifest_id> または chunks/<先頭 2 桁>/<chunk_id>)。
type RelayObject struct {
	Kind      string `json:"kind"`
	Key       string `json:"key"`
	VersionID string `json:"version_id"`
	Size      int64  `json:"size"`
}
