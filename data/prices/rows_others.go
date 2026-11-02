package prices

// 入力の storage の 7〜15 番目(Backblaze から Oracle Cloud Infrastructure まで)。

func b2Spec() rowSpec {
	return rowSpec{ID: "b2-payg", Src: 7, Charges: []charge{
		mk("storage", "6.95", "USD", "tb_month", "1", fStore, "6.95 USD/TB/月、1 TB = 1,000 GB として。最初の 10 GB は無料", fr("10", "gb", "", "absolute")),
		mk("egress", "0.01", "USD", "gb", "1", fEgress, "平均保管量の 3 倍/月まで無料、超過 0.01 USD", fr("3", "gb", "month", "stored_ratio")),
		mk("request", "0.004", "USD", "request", "10000", fReq, "Class D 0.004 USD/10,000 件(1 日 2,500 件まで無料)", ops("class_d"), fr("2500", "request", "day", "absolute")),
	}}
}

func wasabiSpec() rowSpec {
	return rowSpec{ID: "wasabi-hot-payg", Src: 8, Min: minimums{StorageQuantity: tb1(), StorageDays: days(90)}, Charges: []charge{
		mk("storage", "7.99", "USD", "tb_month", "1", fStore, "7.99 USD/TB/月"),
		mk("egress", "0", "USD", "gb", "1", fEgress, "0 USD"),
		mk("request", "0", "USD", "request", "1", fReq, "API 料金 0 USD", ops("all")),
	}}
}

func r2Spec() rowSpec {
	const fr2 = "毎月 Class A 100 万件、Class B 1,000 万件は無料"
	return rowSpec{ID: "r2-standard", Src: 9, Default: true, Charges: []charge{
		mk("storage", "0.015", "USD", "gb_month", "1", fStore, "0.015 USD(毎月 10 GB は無料)", fr("10", "gb", "month", "absolute")),
		mk("egress", "0", "USD", "gb", "1", fEgress, "0 USD"),
		mk("request", "4.50", "USD", "request", "1000000", fReq, "Class A 4.50 USD/1,000,000 件", ops("class_a"), cond(fr2)),
		mk("request", "0.36", "USD", "request", "1000000", fReq, "Class B 0.36 USD/1,000,000 件", ops("class_b"), cond(fr2)),
	}}
}

func scalewaySpec() rowSpec {
	return rowSpec{ID: "scaleway-os-standard-par", Src: 10, Default: true, Charges: []charge{
		mk("storage", "0.01606", "EUR", "gb_month", "1", fStore, "0.01606 EUR"),
		mk("egress", "0.01", "EUR", "gb", "1", fEgress, "0.01 EUR(料金ページの表示では最初の 75 GB/月は無料)", fr("75", "gb", "month", "absolute")),
	}, Skipped: []skipped{sk(10, fReq, "unconfirmed"), sk(10, "minimums", "unconfirmed")}}
}

func hetznerSpec() rowSpec {
	return rowSpec{ID: "hetzner-os", Src: 11, Min: minimums{ObjectSizeKB: days(64)}, Charges: []charge{
		mk("egress", "1.00", "EUR", "tb_month", "1", fEgress, "基本料金に 1 TB/月を含む。超過 1.00 EUR/TB", fr("1", "tb", "month", "absolute")),
	}, Skipped: []skipped{
		sk(11, fStore, "unmapped"), sk(11, fStore, "computed"), sk(11, fReq, "not_applicable"), sk(11, "minimums", "unmapped"),
	}}
}

func idriveSpec() rowSpec {
	return rowSpec{ID: "idrive-e2-payg", Src: 12, Min: minimums{StorageQuantity: tb1()}, Charges: []charge{
		mk("storage", "0.006", "USD", "gb_month", "1", fStore, "0.006 USD"),
		mk("egress", "0.01", "USD", "gb", "1", fEgress, "有効保管量の 3 倍/月まで無料、超過 0.01 USD", fr("3", "gb", "month", "stored_ratio")),
	}, Skipped: []skipped{sk(12, fReq, "not_applicable"), sk(12, "minimums", "unmapped")}}
}

func storjSpec() rowSpec {
	return rowSpec{ID: "storj-standard", Src: 13, Default: true, Min: minimums{StorageDays: days(30), ObjectSizeKB: days(50)}, Charges: []charge{
		mk("storage", "7", "USD", "tb_month", "1", fStore, "7 USD/TB/月"),
		mk("egress", "7", "USD", "tb_month", "1", fEgress, "7 USD/TB"),
	}, Skipped: []skipped{sk(13, fReq, "not_applicable"), sk(13, "minimums", "unmapped")}}
}

func sakuraSpec() rowSpec {
	return rowSpec{ID: "sakura-os-basic", Src: 14, Charges: []charge{
		mk("storage", "49.5", "JPY", "gb_month", "10", fStore, "基本料金に 100 GiB を含む。超過 49.5 円/10 GiB", fr("100", "gb", "", "absolute")),
		mk("egress", "88", "JPY", "gb", "10", fEgress, "基本料金に 10 GiB を含む。超過 88 円/10 GiB", fr("10", "gb", "month", "absolute"),
			cond("さくらインターネットの IP 以外からの GET にだけ発生")),
		mk("request", "55", "JPY", "request", "100000", fReq, "基本料金に 100,000 件を含む。超過 55 円/100,000 件", ops("all"), fr("100000", "request", "month", "absolute")),
	}, Skipped: []skipped{sk(14, fReq, "unmapped"), sk(14, "minimums", "unmapped")}}
}

func ociSpec() rowSpec {
	return rowSpec{ID: "oci-os-standard", Src: 15, Default: true, Charges: []charge{
		mk("storage", "0.0255", "USD", "gb_month", "1", fStore, "0.0255 USD(最初の 10 GB/月は 0 USD)", fr("10", "gb", "month", "absolute")),
		mk("egress", "0.025", "USD", "gb", "1", fEgress, "最初の 10,240 GB/月は 0 USD、超過 0.025 USD", fr("10240", "gb", "month", "absolute"), cond("APAC・日本発")),
		mk("request", "0.0034", "USD", "request", "10000", fReq, "0.0034 USD/10,000 件(最初の 50,000 件/月は無料。区分なし)", ops("all"), fr("50000", "request", "month", "absolute")),
	}}
}

func otherSpecs() []rowSpec {
	return []rowSpec{b2Spec(), wasabiSpec(), r2Spec(), scalewaySpec(), hetznerSpec(), idriveSpec(), storjSpec(), sakuraSpec(), ociSpec()}
}
