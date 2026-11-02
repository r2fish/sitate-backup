package cli

// 契約試験(作業指示書 3.2、5.17 の完了条件 1): 試験ベクトルを骨格に通す。
// 骨格は NOT_IMPLEMENTED の封筒(終了 9)を返し、ここは「未実装」の印で失敗する。U1f-1 は骨格を実装し、この試験は変えない。

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

type envelope struct {
	Schema  string          `json:"schema"`
	Command []string        `json:"command"`
	OK      bool            `json:"ok"`
	DryRun  bool            `json:"dry_run"`
	RunID   *string         `json:"run_id"`
	Result  map[string]any  `json:"result"`
	Error   *envelopeError  `json:"error"`
	Raw     json.RawMessage `json:"-"`
}

type envelopeError struct {
	Code      string         `json:"code"`
	Class     string         `json:"class"`
	Retryable bool           `json:"retryable"`
	Detail    map[string]any `json:"detail"`
}

// run は Main を呼び、標準出力のレコード(1 行 1 個の JSON)と終了コードを返す。骨格の NOT_IMPLEMENTED はここで未実装の印の失敗にする。
func run(t *testing.T, args ...string) ([]envelope, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Main(args, &out, &errOut)
	var recs []envelope
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		var e envelope
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("標準出力の行が JSON でない: %q: %v", line, err)
		}
		e.Raw = json.RawMessage(line)
		recs = append(recs, e)
	}
	last := recs[len(recs)-1]
	if last.Error != nil && last.Error.Code == string(contract.CodeNotImplemented) {
		// 未実装の印の封筒も契約の形でなければならない(スキーマの検証を未実装の印の停止より先に行う)。
		if err := contract.Validate("cli/envelope.json", last.Raw); err != nil {
			t.Fatalf("未実装の封筒がスキーマに合わない: %v\n%s", err, last.Raw)
		}
	}
	if last.Error != nil && last.Error.Code == string(contract.CodeNotImplemented) {
		feature, _ := last.Error.Detail["feature"].(string)
		harness.Implemented(t, contract.ErrNotImplemented(feature))
	}
	return recs, code
}

// runOne は封筒 1 個だけの出力を期待し、スキーマ(cli/envelope.json)に合うことを確かめる。
func runOne(t *testing.T, args ...string) (envelope, int) {
	t.Helper()
	recs, code := run(t, args...)
	if len(recs) != 1 {
		t.Fatalf("標準出力は封筒 1 個のはず: %d 個", len(recs))
	}
	if err := contract.Validate("cli/envelope.json", recs[0].Raw); err != nil {
		t.Fatalf("封筒がスキーマに合わない: %v\n%s", err, recs[0].Raw)
	}
	return recs[0], code
}

type vectors struct {
	ArgErrors []struct {
		Name, Code, Flag, Reason string
		Argv                     []string
	} `json:"arg_errors"`
	DryRunRejected []struct {
		Name string
		Argv []string
	} `json:"dry_run_rejected"`
	GateDenied []struct {
		Name               string
		SSHOriginalCommand string `json:"ssh_original_command"`
		Reason             string
	} `json:"gate_denied"`
}

func loadVectors(t *testing.T) vectors {
	t.Helper()
	b, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// 成功の封筒: 標準出力は 1 個の JSON、ok、終了 0、error は null。
func TestContractCLIEnvelopeSuccess(t *testing.T) {
	e, code := runOne(t, "version")
	if code != 0 || !e.OK || e.Error != nil || e.Schema != "sitate.out/v1" {
		t.Fatalf("version の封筒: exit=%d ok=%v error=%v schema=%q", code, e.OK, e.Error, e.Schema)
	}
	if len(e.Command) != 1 || e.Command[0] != "version" {
		t.Fatalf("command = %v", e.Command)
	}
}

// 失敗の封筒: 終了コードと error.class・retryable が原因コードの表(3.1.3)と終了コードの表(3.1.1)に一致する。
func TestContractCLIErrorEnvelopeMatchesCodeTable(t *testing.T) {
	for _, v := range loadVectors(t).ArgErrors {
		{
			e, code := runOne(t, v.Argv...)
			info := contract.CodeTable[contract.Code(v.Code)]
			if e.OK || e.Error == nil || e.Result != nil {
				t.Fatalf("%s: 失敗の封筒でない: %s", v.Name, e.Raw)
			}
			if e.Error.Code != v.Code || code != info.Exit || e.Error.Class != contract.ExitClass(info.Exit) || e.Error.Retryable != info.Retryable {
				t.Errorf("%s: code=%s exit=%d class=%s retryable=%v(表: exit=%d class=%s retryable=%v)",
					v.Name, e.Error.Code, code, e.Error.Class, e.Error.Retryable, info.Exit, contract.ExitClass(info.Exit), info.Retryable)
			}
			if e.Error.Detail["flag"] != v.Flag || e.Error.Detail["reason"] != v.Reason {
				t.Errorf("%s: detail = %v(flag=%s reason=%s が必要)", v.Name, e.Error.Detail, v.Flag, v.Reason)
			}
		}
	}
}

// --invoked-by timer は受け付ける(init が登録した単位だけが付ける。3.1.1 の呼び出し行と規則 15)。
func TestContractCLIInvokedByTimerAccepted(t *testing.T) {
	e, code := runOne(t, "version", "--invoked-by", contract.TimerInvokedBy)
	if code != 0 || !e.OK {
		t.Fatalf("--invoked-by timer: exit=%d %s", code, e.Raw)
	}
}

// 公開区分の表(3.1.1)のサブコマンドは、test-only を除いてすべて名前が認識される(未知の引数は ARG_INVALID の unknown で、サブコマンドの未知ではない)。
func TestContractCLIEverySubcommandRecognized(t *testing.T) {
	for _, s := range contract.SubcommandTable {
		if s.Public == contract.PublicTestOnly || s.Public == contract.PublicInternal {
			continue
		}
		if s.Words[0] == "bootstrap" || s.Words[0] == "bootstrap-restore" {
			continue // 核のサブコマンドではない(封筒はアプリが作る。spec 3.1.1)。表には道具の一対一のために載っているだけ
		}
		argv := append(append([]string{}, s.Words...), "--nosuch")
		e, code := runOne(t, argv...)
		if code != 2 || e.Error == nil || e.Error.Code != string(contract.CodeArgInvalid) ||
			e.Error.Detail["flag"] != "--nosuch" || e.Error.Detail["reason"] != "unknown" {
			t.Errorf("%v: exit=%d %s", s.Words, code, e.Raw)
		}
		if strings.Join(e.Command, " ") != strings.Join(s.Words, " ") {
			t.Errorf("%v: command = %v", s.Words, e.Command)
		}
	}
}

// --dry-run は subcommands.json の dry_run が真のサブコマンドだけが受け、偽のものには核が ARG_INVALID(flag: --dry-run、unknown。終了 2)を返す(無視しない)。
func TestContractCLIDryRunRejectedOnNonDryRun(t *testing.T) {
	v := loadVectors(t).DryRunRejected
	if len(v) != 5 {
		t.Fatalf("対象外の 5 件(bucket check・notify test・report・source survey・verify)のベクトルが %d 件", len(v))
	}
	for _, c := range v {
		e, code := runOne(t, c.Argv...)
		if code != 2 || e.OK || e.Error == nil || e.Error.Code != string(contract.CodeArgInvalid) || e.Error.Detail["flag"] != "--dry-run" || e.Error.Detail["reason"] != "unknown" {
			t.Errorf("%s: exit=%d %s", c.Name, code, e.Raw)
		}
		if strings.Join(e.Command, " ") != strings.Join(c.Argv[:len(c.Argv)-1], " ") {
			t.Errorf("%s: command = %v", c.Name, e.Command)
		}
	}
	// ベクトルの 5 件は表の dry_run が偽であること(表とベクトルの食い違いを防ぐ)。
	for _, c := range v {
		words := c.Argv[:len(c.Argv)-1]
		for _, s := range contract.SubcommandTable {
			if strings.Join(s.Words, " ") == strings.Join(words, " ") && s.DryRun {
				t.Errorf("%s: 表では dry_run が真", c.Name)
			}
		}
	}
}

// dry_run が真のサブコマンドは --dry-run を拒否しない(ARG_INVALID の --dry-run で返らない)。
func TestContractCLIDryRunAcceptedOnDryRunSubcommands(t *testing.T) {
	n := 0
	for _, s := range contract.SubcommandTable {
		if !s.DryRun {
			continue
		}
		n++
		e, _ := runOne(t, append(append([]string{}, s.Words...), "--dry-run")...)
		if e.Error != nil && e.Error.Code == string(contract.CodeArgInvalid) && e.Error.Detail["flag"] == "--dry-run" {
			t.Errorf("%v: --dry-run を拒否した", s.Words)
		}
	}
	if n != 16 {
		t.Errorf("dry_run が真のサブコマンドは 16 件のはず: %d", n)
	}
}
