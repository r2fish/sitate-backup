package keys

// seal の後の init-storage(--reissue と --dry-run を含む)は提案を要する(spec 3.1.2 の init-storage の行、3.6: storage-init)。
// 直接実行すると APPROVAL_REQUIRED(detail.kind: storage-init)で拒否し、アダプタも保管先も呼ばない。

import (
	"encoding/json"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func TestInitStorageAfterSealNeedsProposal(t *testing.T) {
	for _, extra := range []string{"", "reissue", "dry-run"} {
		e := initEnv(`{"versioning":true,"lock_mode":"COMPLIANCE","default_retention_days":90,"keys":[]}`)
		e.st.Items = map[string]json.RawMessage{"sealed": json.RawMessage("true")}
		c := initCall()
		switch extra {
		case "reissue":
			c.Args["reissue"] = []string{}
		case "dry-run":
			c.DryRun = true
		}
		_, err := e.k.InitStorage(ctx, c)
		harness.ExpectCode(t, err, contract.CodeApprovalRequired)
		if ce, ok := err.(*contract.Error); ok && jsonField(t, ce.Detail, "kind") != "storage-init" {
			t.Errorf("%s: detail.kind = %s", extra, ce.Detail)
		}
		if n := e.run.called("create-bucket") + len(e.ks.Reqs); n != 0 {
			t.Errorf("%s: 拒否のはずがアダプタ・発行を呼んだ: %d", extra, n)
		}
	}
}
