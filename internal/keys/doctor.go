package keys

// doctor(spec 3.1.2「doctor の検査項目」): 状態ファイルから決まる現在の段階(bootstrap < defined < sealed)までの項目と advisory を表の順に検査する。
// OS の実測は OSProbe、保管先の応答の Date は ObjectStore.StoreTime。時刻のずれは試験ビルドの規則「Date + オフセット」で判定する(オフセットを持つ時計だけ contract.Offsetter を実装する)。

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

type checkEl struct {
	Name     string          `json:"name"`
	Severity string          `json:"severity"`
	OK       bool            `json:"ok"`
	Next     []contract.Next `json:"next"`
	Count    *int            `json:"count,omitempty"`
}

// doctorStage は状態ファイルから決まる現在の段階。
func (k *Keys) doctorStage(ctx context.Context) (string, error) {
	var sealed bool
	if _, err := k.getItem(ctx, "sealed", &sealed); err != nil {
		return "", err
	}
	if sealed {
		return "sealed", nil
	}
	if _, _, found, err := k.State.ReadCandidate(ctx); err != nil {
		return "", err
	} else if found {
		return "defined", nil
	}
	return "bootstrap", nil
}

// Doctor は契約 contract.Keys を満たす。state_dir_fs が NFS・CIFS・FUSE なら LOCK_UNAVAILABLE(終了 3)で止める。
func (k *Keys) Doctor(ctx context.Context, c contract.Call) (contract.Reply, error) {
	if err := errNeedDeps(k.State != nil, k.Clock != nil, k.OS != nil); err != nil {
		return contract.Reply{}, err
	}
	stage, err := k.doctorStage(ctx)
	if err != nil {
		return contract.Reply{}, err
	}
	if err := k.stateDirFS(ctx); err != nil {
		return contract.Reply{}, err
	}
	checks := []checkEl{}
	passed := true
	for _, dc := range contract.DoctorChecks {
		if dc.Stage != "all" && slices.Index(contract.DoctorStages, dc.Stage) > slices.Index(contract.DoctorStages, stage) {
			continue
		}
		ok, err := k.runCheck(ctx, dc.Name)
		if err != nil {
			return contract.Reply{}, err
		}
		el := checkEl{Name: dc.Name, Severity: dc.Severity, OK: ok, Next: []contract.Next{}}
		if dc.Name == "check_images" {
			n := contract.DoctorCheckImagesCount
			el.Count = &n
		}
		if !ok {
			el.Next = []contract.Next{{Argv: []string{"sitate", "doctor"}, RequiresHuman: true, Why: nil}}
			if dc.Severity == "required" {
				passed = false
			}
		}
		checks = append(checks, el)
	}
	return reply(map[string]any{"stage": stage, "passed": passed, "checks": checks})
}

// stateDirFS は状態ディレクトリのファイルシステムを確かめる(NFS・CIFS・FUSE は止める)。OSProbe が無ければ確かめない。
func (k *Keys) stateDirFS(ctx context.Context) error {
	fs, err := k.OS.FSType(ctx, k.Dir)
	if err != nil {
		return err
	}
	if slices.Contains(contract.FSTypesStateForbidden, fs) {
		return mkErr(contract.CodeLockUnavailable, map[string]any{"reason": "unsupported_fs", "fs_type": fs})
	}
	return nil
}

func (k *Keys) runCheck(ctx context.Context, name string) (bool, error) {
	switch name {
	case "state_dir_fs", "check_images":
		return true, nil // state_dir_fs は stateDirFS が止める。check_images はイテレーション 1 では対象 0 件で合格
	case "storage_reachable":
		return k.storageReachable(ctx), nil
	case "time_skew":
		return k.timeSkewOK(ctx)
	}
	ok, _, err := k.OS.Check(ctx, name)
	return ok, err
}

// storageReachable は保管先に届いて応答を受けたか(応答の状態コードは問わない)。
func (k *Keys) storageReachable(ctx context.Context) bool {
	if k.Store == nil {
		return false
	}
	_, err := k.Store.BucketConfig(ctx)
	if err == nil {
		return true
	}
	var ce *contract.Error
	return errors.As(err, &ce) && ce.Code != contract.CodeStorageUnreachable
}

// timeSkewOK は保管先の応答の Date に注入したオフセットを足した時刻とバックアップサーバーの時計の差が TimeSkewMax 以内か。
func (k *Keys) timeSkewOK(ctx context.Context) (bool, error) {
	if k.Store == nil {
		return false, nil
	}
	st, err := k.Store.StoreTime(ctx)
	if err != nil {
		return false, nil
	}
	now, err := k.Clock.Now(ctx)
	if err != nil {
		return false, err
	}
	var off time.Duration
	if o, ok := k.Clock.(contract.Offsetter); ok {
		if off, err = o.Offset(); err != nil {
			return false, err
		}
	}
	d := now.Sub(st.Add(off))
	if d < 0 {
		d = -d
	}
	return d <= contract.TimeSkewMax, nil
}
