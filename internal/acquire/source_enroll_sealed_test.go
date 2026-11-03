package acquire

// seal の後の source enroll(--rotate を含む)は提案を要する(spec 3.1.2 の source enroll の行: source-change)。
// 直接実行すると、定義も SSH も使う前に APPROVAL_REQUIRED(detail.kind: source-change)で拒否する。apply の Enroller は止めない。

import (
	"encoding/json"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
)

func TestSourceEnrollAfterSealNeedsProposal(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		st := &fake.StateFiles{Items: map[string]json.RawMessage{"sealed": json.RawMessage("true")}}
		a := &Acquire{State: st, SSH: &fake.SSH{}}
		c := sourceCall(enrollWords, "ssh://root@192.0.2.1", newAdminKey(t))
		if rotate {
			c.Args["rotate"] = []string{}
		}
		_, err := a.SourceEnroll(ctx, c)
		ce, ok := err.(*contract.Error)
		if !ok || ce.Code != contract.CodeApprovalRequired {
			t.Fatalf("rotate=%v: APPROVAL_REQUIRED のはず: %v", rotate, err)
		}
		var d struct {
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal(ce.Detail, &d)
		if d.Kind != "source-change" {
			t.Errorf("detail.kind = %s", ce.Detail)
		}
	}
}
