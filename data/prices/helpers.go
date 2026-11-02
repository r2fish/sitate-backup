package prices

// rows_*.go の表を短く書くための道具。

type opt func(*charge)

// mk は費目を作る。per は件数・量の単位(文に数が無ければ "1")。
func mk(item, amount, cur, unit, per, field, text string, opts ...opt) charge {
	c := charge{Item: item, Amount: amount, Currency: cur, Unit: unit, Per: per, SrcField: field, SrcText: text}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func tier(q, u string) opt { return func(c *charge) { c.TierUpto = &qty{q, u} } }

func ops(o ...string) opt { return func(c *charge) { c.Ops = o } }

func cond(s string) opt { return func(c *charge) { c.Cond = &s } }

// fr は無料枠。period は "month" / "day" / ""(null)。
func fr(q, u, period, basis string) opt {
	return func(c *charge) {
		f := &free{Quantity: q, Unit: u, Basis: basis}
		if period != "" {
			p := period
			f.Period = &p
		}
		c.Free = f
	}
}

func sk(idx int, field, reason string) skipped {
	return skipped{Src: srcRef{"storage", idx}, SrcField: field, Reason: reason}
}

func days(n int) *int { return &n }

func tb1() *qty { return &qty{"1", "tb"} }

const (
	fStore  = "storage_price_per_gb_month"
	fEgress = "egress_price_per_gb"
	fReq    = "request_price"
)
