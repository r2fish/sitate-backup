package cli

import "testing"

// 作業指示書 5.17 の完了条件 8 / spec 3.1.2.8 の 4: --daily-gb・--dump-gb を省くと --size-gb に要件書の標準例の比
// (100 GB に対し日次変更 1 GB、ダンプ 5 GB)を掛けて埋め、片方だけ与えたらもう片方は埋めたまま。basis は args。
func TestEstimateOmittedFlagsScaleWithSizeAndEachOverrides(t *testing.T) {
	f := estimateEnv(t)
	ep := []string{"https://s3.us-west-004.example-b2.test"}
	cases := []struct {
		name        string
		args        map[string][]string
		size, daily float64
		dump        float64
	}{
		{"両方省く(200 GB)", map[string][]string{"endpoint": ep, "size-gb": {"200"}}, 200, 2, 10},
		{"ダンプだけ与える", map[string][]string{"endpoint": ep, "size-gb": {"200"}, "dump-gb": {"7"}}, 200, 2, 7},
		{"日次だけ与える", map[string][]string{"endpoint": ep, "size-gb": {"200"}, "daily-gb": {"3"}}, 200, 3, 10},
	}
	for _, c := range cases {
		out, _, err := estimateRun(t, f, c.args)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		in := out["inputs"].(map[string]any)
		if out["basis"] != "args" || in["size_gb"] != c.size || in["daily_gb"] != c.daily || in["dump_gb"] != c.dump {
			t.Errorf("%s: basis=%v inputs=%v", c.name, out["basis"], in)
		}
	}
}
