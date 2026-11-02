package prices

// AWS の S3 3 行(入力の storage の 0〜2 番目)。
// 「最初の N」「次の N」は幅で書かれた段で、tier_upto は累積の上限にする(spec 7.2。D-0159)。

const (
	awsEgress0 = "0.114 USD(最初の 10 TB/月。全世界合計 100 GB/月の無料枠の超過分)"
	awsEgress1 = "0.089 USD(次の 40 TB)"
	awsEgress2 = "0.086 USD(次の 100 TB)"
	awsEgress3 = "0.084 USD(150 TB 超)"
)

func awsEgress() []charge {
	return []charge{
		mk("egress", "0.114", "USD", "gb", "1", fEgress, awsEgress0, tier("10", "tb"), fr("100", "gb", "month", "absolute")),
		mk("egress", "0.089", "USD", "gb", "1", fEgress, awsEgress1, tier("50", "tb")),
		mk("egress", "0.086", "USD", "gb", "1", fEgress, awsEgress2, tier("150", "tb")),
		mk("egress", "0.084", "USD", "gb", "1", fEgress, awsEgress3),
	}
}

func awsReq(w, wText, r, rText string) []charge {
	return []charge{
		mk("request", w, "USD", "request", "1000", fReq, wText, ops("PUT", "COPY", "POST", "LIST")),
		mk("request", r, "USD", "request", "10000", fReq, rText, ops("GET")),
	}
}

func awsSpecs() []rowSpec {
	std := append([]charge{
		mk("storage", "0.025", "USD", "gb_month", "1", fStore, "0.025 USD(最初の 50 TB)", tier("50", "tb")),
		mk("storage", "0.024", "USD", "gb_month", "1", fStore, "0.024 USD(次の 450 TB)", tier("500", "tb")),
		mk("storage", "0.023", "USD", "gb_month", "1", fStore, "0.023 USD(500 TB 超)"),
	}, awsEgress()...)
	std = append(std, awsReq("0.0047", "PUT・COPY・POST・LIST 0.0047 USD/1,000 件", "0.0037", "GET ほか 0.0037 USD/10,000 件")...)

	ia := append([]charge{mk("storage", "0.0138", "USD", "gb_month", "1", fStore, "0.0138 USD")}, awsEgress()...)
	ia = append(ia, mk("retrieval", "0.01", "USD", "gb", "1", fEgress, "別に取り出し料 0.01 USD/GB"))
	ia = append(ia, awsReq("0.01", "PUT・COPY・POST・LIST 0.01 USD/1,000 件", "0.01", "GET ほか 0.01 USD/10,000 件")...)

	gir := append([]charge{mk("storage", "0.005", "USD", "gb_month", "1", fStore, "0.005 USD")}, awsEgress()...)
	gir = append(gir, mk("retrieval", "0.03", "USD", "gb", "1", fEgress, "別に取り出し料 0.03 USD/GB"))
	gir = append(gir, awsReq("0.02", "PUT・COPY・POST・LIST 0.02 USD/1,000 件", "0.1", "GET ほか 0.1 USD/10,000 件")...)

	return []rowSpec{
		{ID: "aws-s3-standard-apne1", Src: 0, Default: true, Charges: std},
		{ID: "aws-s3-standard-ia-apne1", Src: 1, Min: minimums{StorageDays: days(30), ObjectSizeKB: days(128)}, Charges: ia},
		{ID: "aws-s3-glacier-ir-apne1", Src: 2, Min: minimums{StorageDays: days(90), ObjectSizeKB: days(128)}, Charges: gir},
	}
}
