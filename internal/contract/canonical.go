package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/gowebpki/jcs"
)

// CanonicalJSON は JSON を RFC 8785(JCS)で直列化する。canonical_hash・def_hash・合格記録の署名の正規化で共有する(作業指示書 5.1 GB-85)。
// 各部品がそれぞれ JCS を呼ぶと数値や文字列の扱いが割れるため、直列化はここだけに置く。
func CanonicalJSON(b []byte) ([]byte, error) { return jcs.Transform(b) }

// HashPrefix は sha256 のハッシュの書き方(spec 4.6)。
const HashPrefix = "sha256:"

// CanonicalHash は JSON の JCS 直列化の SHA-256 を `sha256:<小文字 16 進 64 文字>` で返す。
// def_hash は正規化した定義(書いた項目だけ。既定値を補わない。spec 4.1)にこれを掛けたもの。
func CanonicalHash(b []byte) (string, error) {
	c, err := CanonicalJSON(b)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(c)
	return HashPrefix + hex.EncodeToString(h[:]), nil
}

// AddMonthsUTC は UTC の暦の月加算。加算した月に同じ日が無ければその月の末日に丸める(時刻はそのまま)。
// 月次の retain-until(作成時刻 + 12 か月)と合格記録の retention_until に使う(spec 4.2「retain-until の計算」)。
// 例: 2028-02-29T03:00:00Z + 12 か月 = 2029-02-28T03:00:00Z。
func AddMonthsUTC(t time.Time, n int) time.Time {
	t = t.UTC()
	first := time.Date(t.Year(), t.Month()+time.Month(n), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(t.Day(), last), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}
