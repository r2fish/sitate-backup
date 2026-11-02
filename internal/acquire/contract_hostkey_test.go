package acquire

// 契約試験(作業指示書 5.4 の 3): 照らすホスト鍵の値の選択(spec 3.3「ホスト鍵」、7.5 Surveyor)を、組ごとの表で確かめる。
// 入力の SourceSeen・HostKeys は、呼び出し側が Host の接続先で絞った指紋の並び(spec の入力は「接続先の <host> と、その接続先の source_seen[]・host_keys[]」)。
// 時機の文字列は契約に値が無い(足りない契約。報告)。試験は survey / enroll / apply を使う。

import (
	"encoding/json"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

const (
	timingSurvey = "survey"
	timingEnroll = "enroll"
	timingApply  = "apply"

	fpA = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	fpB = "SHA256:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	fpC = "SHA256:CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"
)

type hkWant int

const (
	hkSkip     hkWant = iota // 照らさない
	hkCompare                // 照らす(FP が値)
	hkDefBad                 // DEF_INVALID(/source/host)
	hkNoRecord               // SOURCE_HOSTKEY_MISMATCH(expected_fp: null)
)

func TestContractSurveyorSelectHostKey(t *testing.T) {
	const h, other = "src.example.invalid", "new.example.invalid"
	cases := []struct {
		name string
		in   contract.HostKeyChoiceInput
		want hkWant
		fp   string
	}{
		{"survey: seal の後で固定値があれば固定値", contract.HostKeyChoiceInput{Host: h, Timing: timingSurvey, DefHost: h, Sealed: true, HostKeys: []string{fpA}, SourceSeen: []string{fpB}}, hkCompare, fpA},
		{"survey: seal の後でも固定値が無い接続先(移転先の新しいホスト)は照らさない", contract.HostKeyChoiceInput{Host: other, Timing: timingSurvey, DefHost: h, Sealed: true}, hkSkip, ""},
		{"survey: seal の前は記録があっても照らさない", contract.HostKeyChoiceInput{Host: h, Timing: timingSurvey, DefHost: h, HostKeys: []string{fpA}, SourceSeen: []string{fpB}}, hkSkip, ""},
		{"survey: seal の前で記録も無ければ照らさない(最初の調査)", contract.HostKeyChoiceInput{Host: h, Timing: timingSurvey, DefHost: h}, hkSkip, ""},
		{"enroll: 接続先が定義の source.host と違えば DEF_INVALID", contract.HostKeyChoiceInput{Host: other, Timing: timingEnroll, DefHost: h, SourceSeen: []string{fpB}, HostKeys: []string{fpA}}, hkDefBad, ""},
		{"enroll: source_seen があればそれ(固定値より先)", contract.HostKeyChoiceInput{Host: h, Timing: timingEnroll, DefHost: h, SourceSeen: []string{fpB}, HostKeys: []string{fpA}}, hkCompare, fpB},
		{"enroll: source_seen が無ければ固定値", contract.HostKeyChoiceInput{Host: h, Timing: timingEnroll, DefHost: h, HostKeys: []string{fpA}}, hkCompare, fpA},
		{"enroll: 記録も固定値も無ければ最初の接続として受けない", contract.HostKeyChoiceInput{Host: h, Timing: timingEnroll, DefHost: h}, hkNoRecord, ""},
		{"apply: 提案の host_key_fp と照らす", contract.HostKeyChoiceInput{Host: h, Timing: timingApply, DefHost: h, Sealed: true, HostKeys: []string{fpA}, SourceSeen: []string{fpB}, ProposalFP: fpC}, hkCompare, fpC},
		{"apply: 移転先の接続先でも提案の host_key_fp と照らす", contract.HostKeyChoiceInput{Host: other, Timing: timingApply, DefHost: h, Sealed: true, ProposalFP: fpC}, hkCompare, fpC},
	}
	_, perr := (&Surveyor{}).SelectHostKey(ctx, cases[0].in)
	harness.Implemented(t, perr) // 親にも未実装の印を残す(契約試験の集計は親ごとに原因を見る)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := (&Surveyor{}).SelectHostKey(ctx, c.in)
			harness.Implemented(t, err)
			if err != nil {
				t.Fatalf("選択は誤りを Err に載せ、err は返さない: %v", err)
			}
			checkChoice(t, got, c.want, c.fp)
		})
	}
}

func checkChoice(t *testing.T, got contract.HostKeyChoice, want hkWant, fp string) {
	t.Helper()
	switch want {
	case hkSkip:
		if got.Compare || got.Err != nil || got.FP != "" {
			t.Errorf("照らさない: %+v", got)
		}
	case hkCompare:
		if !got.Compare || got.Err != nil || got.FP != fp {
			t.Errorf("照らす指紋は %s: %+v", fp, got)
		}
	case hkDefBad:
		if got.Compare || got.Err == nil {
			t.Fatalf("DEF_INVALID を期待: %+v", got)
		}
		harness.ExpectCode(t, got.Err, contract.CodeDefInvalid)
		var d struct {
			Issues []struct{ Pointer, Reason string }
		}
		_ = json.Unmarshal(got.Err.Detail, &d)
		if len(d.Issues) != 1 || d.Issues[0].Pointer != "/source/host" || d.Issues[0].Reason != "invalid_value" {
			t.Errorf("issues は /source/host の invalid_value 1 件: %s", got.Err.Detail)
		}
	case hkNoRecord:
		if got.Compare || got.Err == nil {
			t.Fatalf("SOURCE_HOSTKEY_MISMATCH を期待: %+v", got)
		}
		harness.ExpectCode(t, got.Err, contract.CodeSourceHostkeyMismatch)
		var d map[string]json.RawMessage
		_ = json.Unmarshal(got.Err.Detail, &d)
		if v, ok := d["expected_fp"]; !ok || string(v) != "null" {
			t.Errorf("expected_fp は null で出す: %s", got.Err.Detail)
		}
	}

}

func TestContractSurveyorSelectHostKeyUnknownTiming(t *testing.T) {
	c, err := (&Surveyor{}).SelectHostKey(ctx, contract.HostKeyChoiceInput{Host: "src.example.invalid", Timing: "nosuch"})
	harness.Implemented(t, err)
	if err == nil && c.Err == nil {
		t.Error("知らない時機は誤り(Err)を返す")
	}
	if c.Compare {
		t.Errorf("知らない時機で照らす値を返した: %+v", c)
	}
}
