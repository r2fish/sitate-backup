package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"time"
)

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// 秘密(フレーズ)は、どの終わり方でも出力した後に消す。失敗の封筒には添えない。
func TestDeliverPhraseWipesAndNeverAttachesToFailure(t *testing.T) {
	d := &Deps{}
	// 失敗の封筒: 何も書かず、消す。
	var out bytes.Buffer
	sec := contract.NewSecret([]byte(`{"words":"a b"}`))
	o := d.deliverPhrase(outcome{phrase: sec, err: newErr(contract.CodeStorageUnreachable, nil)}, &out, secretStdout, nil)
	if out.Len() != 0 || o.phraseEmitted || sec.Reveal() != nil {
		t.Errorf("失敗: out=%q emitted=%v wiped=%v", out.String(), o.phraseEmitted, sec.Reveal() == nil)
	}
	// 成功: 書いて、消す。
	out.Reset()
	sec = contract.NewSecret([]byte(`{"words":"a b"}`))
	o = d.deliverPhrase(outcome{phrase: sec}, &out, secretStdout, nil)
	if out.String() != "{\"words\":\"a b\"}\n" || !o.phraseEmitted || sec.Reveal() != nil {
		t.Errorf("成功: out=%q emitted=%v", out.String(), o.phraseEmitted)
	}
	// 書けない: INTERNAL、phrase_emitted は立てない、消す。
	sec = contract.NewSecret([]byte(`{"words":"a b"}`))
	o = d.deliverPhrase(outcome{phrase: sec}, failWriter{}, secretStdout, nil)
	if o.err == nil || o.err.Code != contract.CodeInternal || o.phraseEmitted || sec.Reveal() != nil {
		t.Errorf("書けない: err=%v emitted=%v", o.err, o.phraseEmitted)
	}
	// JSON でない秘密は出さない。
	out.Reset()
	sec = contract.NewSecret([]byte(`not json`))
	o = d.deliverPhrase(outcome{phrase: sec}, &out, secretStdout, nil)
	if o.phraseEmitted || o.err == nil {
		t.Errorf("JSON でない秘密を出した: %q", out.String())
	}
}

func TestFinishWipesPhraseOnErrorAndWithoutOutput(t *testing.T) {
	sec := contract.NewSecret([]byte("x"))
	o := finish(outcome{}, replyOrErr{rep: contract.Reply{Phrase: sec}, err: newErr(contract.CodeStorageUnreachable, nil)}, true)
	if o.err == nil || sec.Reveal() != nil {
		t.Errorf("誤りと一緒に返った秘密を消さない")
	}
	sec = contract.NewSecret([]byte("x"))
	o = finish(outcome{}, replyOrErr{rep: contract.Reply{Phrase: sec}}, false)
	if o.err == nil || o.err.Code != contract.CodeInternal || sec.Reveal() != nil {
		t.Errorf("出力先が無いのに返った秘密: %v", o.err)
	}
}

// 中止の実行が返した秘密は消し、封筒に載せない。
func TestAbortWipesPhrase(t *testing.T) {
	f := newFlow(t)
	f.d.Limits.MaxDuration = 40 * time.Millisecond
	f.d.Lease = &Lease{Dir: f.d.StateDir, MaxDuration: 40 * time.Millisecond}
	sec := contract.NewSecret([]byte("x"))
	o := f.run(t, []string{"backup"}, func(_ *Deps, c context.Context, _ contract.Call) (contract.Reply, error) {
		<-c.Done()
		return contract.Reply{Phrase: sec}, c.Err()
	})
	if o.err == nil || o.err.Code != contract.CodeRunAborted || sec.Reveal() != nil {
		t.Errorf("err=%v", o.err)
	}
	_ = io.Discard
}
