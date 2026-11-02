// Command fakes3 は偽の保管先を独立の S3 互換サーバーとして起動する(make test-env-up KIND=fake)。
// 模擬時間の共有の値(環境変数 SITATE_SIM_TIME_FILE)は、時刻を読むたびに読む。無い・壊れているときは内部エラー(5xx)を返す。
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/fake/s3server"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	listen := flag.String("listen", ":9000", "待ち受け")
	ak := flag.String("access-key", "", "管理用鍵のアクセスキー")
	sk := flag.String("secret-key", "", "管理用鍵の秘密")
	allowUnv := flag.Bool("allow-unversioned-delete", false, "書き込み鍵の版指定なしの削除を許す")
	var extra multi
	flag.Var(&extra, "key", "追加の鍵 <アクセスキー>:<秘密>:<write|read|admin>(複数可)")
	flag.Parse()
	if *ak == "" || *sk == "" {
		log.Fatal("-access-key と -secret-key が要ります")
	}
	path := os.Getenv(contract.EnvSimTimeFile)
	clock := fake.FuncClock(func() (time.Time, error) {
		if path == "" {
			return time.Time{}, contract.ErrSimTimeInvalid
		}
		st, err := contract.ReadSimTime(path)
		if err != nil {
			return time.Time{}, err
		}
		return st.Now(time.Now())
	})
	core := fake.NewCore(clock)
	core.AllowUnversionedDelete = *allowUnv
	srv := s3server.New(core, *ak, *sk)
	for _, e := range extra {
		p := strings.Split(e, ":")
		if len(p) != 3 {
			log.Fatalf("-key の形が違う: %s", fmt.Sprint(len(p)))
		}
		srv.Keys[p[0]] = s3server.Key{Secret: p[1], Role: p[2]}
	}
	log.Printf("fakes3 listening %s", *listen)
	log.Fatal(http.ListenAndServe(*listen, srv))
}
