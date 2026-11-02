package contract

import (
	"context"
	"time"
)

// Clock は核の時計(spec 7.5。試験ビルドの「Date + 注入したオフセット」の判定に要るオフセットを各部品へ渡す形)。
// 製品ビルドは時計を変える設定・環境変数・引数を持たない。
type Clock interface {
	// Now は現在時刻(UTC、秒まで)。apply の源は ObjectStore.StoreTime、他はバックアップサーバーの時計。
	Now(ctx context.Context) (time.Time, error)
}

// Offsetter は保管先の応答の Date に足す模擬時間のオフセットを与える(試験ビルドだけが実装を持つ)。
type Offsetter interface {
	// Offset は Date に足すオフセット(共有の値を読むたびに読み直す)。
	Offset() (time.Duration, error)
}

// RetentionParams は保持の定数(日次の保持期間・月次の保持期間)。日次 90 日・月次 12 か月が固定で、
// 変えられるのは試験ビルドだけ(試験用の保持日数の与え方は EnvTestRetentionDailyDays と EnvTestRetentionMonthlyMonths)。
type RetentionParams interface {
	DailyDays() int
	MonthlyMonths() int
}

// FixedRetention は製品ビルドの保持の定数。
type FixedRetention struct{}

// DailyDays は日次の保持期間(日)。
func (FixedRetention) DailyDays() int { return RetentionDailyDays }

// MonthlyMonths は月次の保持期間(か月)。
func (FixedRetention) MonthlyMonths() int { return RetentionMonthlyMonths }
