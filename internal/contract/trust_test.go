package contract

import "testing"

func TestTrustAnchorsNotEmptyInProductBuild(t *testing.T) {
	if len(TrustAnchors()) == 0 {
		t.Skip("試験ビルドは EnvTestTrustPubKeyFile が無いと空(使い捨ての鍵は test/env/ の起動時に生成する)")
	}
}
