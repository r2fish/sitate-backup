package keys

// D-0160(spec 3.1.2 の bucket check): --key-ref を省くと定義の storage[0].key_ref を使う。seal の前は候補の値。候補が無ければ DEF_INVALID。

import (
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

func noKeyRefCall() contract.Call {
	return contract.Call{Words: []string{"bucket", "check"}, SecretOut: true}
}

func TestBucketCheckWithoutKeyRefUsesCandidateStorageKeyRef(t *testing.T) {
	e := newEnv()
	goodBucket(e)
	e.st.cand, e.st.candDef = true, candStorage
	r, err := e.k.BucketCheck(ctx, noKeyRefCall())
	harness.Shape(t, []string{"bucket", "check"}, r, err)
	wantPermissions(t, perms(t, r))
}

func TestBucketCheckWithoutKeyRefAndWithoutCandidateIsDefInvalid(t *testing.T) {
	e := newEnv()
	goodBucket(e)
	_, err := e.k.BucketCheck(ctx, noKeyRefCall())
	harness.ExpectCode(t, err, contract.CodeDefInvalid)
	if len(e.pr.Puts) != 0 {
		t.Errorf("候補が無いのに保管先へ書いた: %v", e.pr.Puts)
	}
}
