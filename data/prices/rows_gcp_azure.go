package prices

// Google Cloud の 2 行(入力の storage の 3・4 番目)と Azure(5・6 番目。6 番目は Cool と Cold の 2 行に分ける)。

const gcpAsia = "アジア向け(中国を除く)"

func gcpEgress() []charge {
	return []charge{
		mk("egress", "0.12", "USD", "gb", "1", fEgress, "0.12 USD/GiB(0〜10 TiB)", tier("10", "tb"), cond(gcpAsia)),
		mk("egress", "0.11", "USD", "gb", "1", fEgress, "0.11 USD/GiB(10〜150 TiB)", tier("150", "tb"), cond(gcpAsia)),
		mk("egress", "0.08", "USD", "gb", "1", fEgress, "0.08 USD/GiB(150 TiB 超)", cond(gcpAsia)),
	}
}

func gcpSpecs() []rowSpec {
	std := append([]charge{mk("storage", "0.023", "USD", "gb_month", "1", fStore, "0.023 USD/GiB")}, gcpEgress()...)
	std = append(std,
		mk("request", "0.005", "USD", "request", "1000", fReq, "Class A 0.005 USD/1,000 件", ops("class_a")),
		mk("request", "0.0004", "USD", "request", "1000", fReq, "Class B 0.0004 USD/1,000 件", ops("class_b")))
	cold := append([]charge{mk("storage", "0.006", "USD", "gb_month", "1", fStore, "0.006 USD/GiB")}, gcpEgress()...)
	cold = append(cold,
		mk("retrieval", "0.02", "USD", "gb", "1", fEgress, "別に取り出し料 0.02 USD/GiB"),
		mk("request", "0.02", "USD", "request", "1000", fReq, "Class A 0.02 USD/1,000 件", ops("class_a")),
		mk("request", "0.01", "USD", "request", "1000", fReq, "Class B 0.01 USD/1,000 件", ops("class_b")))
	return []rowSpec{
		{ID: "gcp-gcs-standard-asne1", Src: 3, Default: true, Charges: std},
		{ID: "gcp-gcs-coldline-asne1", Src: 4, Min: minimums{StorageDays: days(90)}, Charges: cold},
	}
}

const azNet = "Microsoft ネットワーク経由"

// azEgress は Microsoft ネットワーク経由の段だけを写す(インターネット経路の段は同じ欄の別の経路で、足すと二重に数える。報告)。
func azEgress() []charge {
	return []charge{
		mk("egress", "0.12", "USD", "gb", "1", fEgress, "最初の 100 GB/月は無料。Microsoft ネットワーク経由 0.12 USD(〜約 10 TB)",
			tier("10", "tb"), fr("100", "gb", "month", "absolute"), cond(azNet)),
		mk("egress", "0.085", "USD", "gb", "1", fEgress, "0.085 USD(〜約 50 TB)", tier("50", "tb"), cond(azNet)),
		mk("egress", "0.082", "USD", "gb", "1", fEgress, "0.082 USD(〜約 150 TB)", tier("150", "tb"), cond(azNet)),
		mk("egress", "0.08", "USD", "gb", "1", fEgress, "0.08 USD(以降)", cond(azNet)),
	}
}

func azReq(w, wt, r, rt string) []charge {
	return []charge{
		mk("request", w, "USD", "request", "10000", fReq, wt, ops("write")),
		mk("request", r, "USD", "request", "10000", fReq, rt, ops("read")),
	}
}

func azureSpecs() []rowSpec {
	hot := append([]charge{
		mk("storage", "0.02", "USD", "gb_month", "1", fStore, "0.02 USD(最初の 50 TB)", tier("50", "tb")),
		mk("storage", "0.0192", "USD", "gb_month", "1", fStore, "0.0192 USD(次の 450 TB)", tier("450", "tb")),
		mk("storage", "0.0184", "USD", "gb_month", "1", fStore, "0.0184 USD(500 TB 超)"),
	}, azEgress()...)
	hot = append(hot, azReq("0.05", "書き込み 0.05 USD/10,000 件", "0.004", "読み取り 0.004 USD/10,000 件")...)
	hot = append(hot,
		mk("request", "0.05", "USD", "request", "10000", fReq, "一覧とコンテナ作成 0.05 USD/10,000 件", ops("list")),
		mk("request", "0.004", "USD", "request", "10000", fReq, "その他 0.004 USD/10,000 件", ops("other")))

	cool := append([]charge{mk("storage", "0.011", "USD", "gb_month", "1", fStore, "Cool 0.011 USD")}, azEgress()...)
	cool = append(cool, mk("retrieval", "0.01", "USD", "gb", "1", fEgress, "取り出し料 Cool 0.01 USD/GB"))
	cool = append(cool, azReq("0.1", "Cool: 書き込み 0.1 USD/10,000 件", "0.01", "読み取り 0.01 USD/10,000 件")...)

	cold := append([]charge{mk("storage", "0.0045", "USD", "gb_month", "1", fStore, "Cold 0.0045 USD")}, azEgress()...)
	cold = append(cold, mk("retrieval", "0.03", "USD", "gb", "1", fEgress, "Cold 0.03 USD/GB"))
	cold = append(cold, azReq("0.18", "Cold: 書き込み 0.18 USD/10,000 件", "0.1", "読み取り 0.1 USD/10,000 件")...)

	return []rowSpec{
		{ID: "azure-blob-hot-japaneast", Src: 5, Default: true, Charges: hot},
		{ID: "azure-blob-cool-japaneast", Src: 6, Plan: "Blob Storage Cool LRS(Japan East)", Min: minimums{StorageDays: days(30)}, Charges: cool},
		{ID: "azure-blob-cold-japaneast", Src: 6, Plan: "Blob Storage Cold LRS(Japan East)", Min: minimums{StorageDays: days(90)}, Charges: cold},
	}
}

func gcpAzureSpecs() []rowSpec { return append(gcpSpecs(), azureSpecs()...) }
