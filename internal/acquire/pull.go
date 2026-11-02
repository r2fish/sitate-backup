package acquire

// pull 接続(sitate-ro での受け口の呼び出し)と、source enroll の自己診断(spec 3.3「source enroll の自己診断」)。

import (
	"bytes"
	"context"
	"io"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// pullRun は受け口の操作を 1 回呼ぶ(argv の 1 つのコマンドとして送る)。stdin は read-batch のパス一覧など。
func pullRun(ctx context.Context, s contract.SSH, conn contract.SSHConn, stdin []byte, argv ...string) (sshOut, error) {
	req := contract.SSHRequest{SSHConn: conn, Argv: argv}
	if stdin != nil {
		req.Stdin = bytes.NewReader(stdin)
	}
	var buf bytes.Buffer
	req.Stdout = io.Writer(&buf)
	r, err := s.Run(ctx, req)
	return sshOut{Out: buf.Bytes(), Exit: r.ExitStatus, SeenFP: r.SeenFP}, err
}

// pullErr は pull 接続の失敗を原因コードにする(固定値と違えば SOURCE_HOSTKEY_MISMATCH、届かなければ STORAGE_UNREACHABLE)。
func pullErr(err error) error {
	return mapConnErr(err, func(string) *contract.Error {
		return errSetup(setupDetail{Step: "authorized_keys", Reason: ""})
	})
}

// pullChecks は sitate-ro の鍵で受け口の selftest を実行し、拒否の検査と DB の試行をして selftest[] の項目を作る。
func (d enrollDeps) pullChecks(ctx context.Context, h hostSpec, key pullKey, fp, wantHash string, def checkDef) ([]selfItem, error) {
	conn := pullConn(h.Host, key, fp)
	o, err := pullRun(ctx, d.SSH, conn, nil, "selftest")
	if err != nil {
		return nil, pullErr(err)
	}
	if o.Exit != 0 {
		return nil, errInternal("exit_code")
	}
	doc, err := parseSelfDoc(o.Out)
	if err != nil {
		return nil, err
	}
	items := evalSelftest(doc, wantHash, def)
	deny, err := d.denyChecks(ctx, conn, def)
	if err != nil {
		return nil, err
	}
	items = append(items, deny...)
	for _, t := range doc.Targets {
		if t.Kind != "postgresql" || !inDef(def, t.ID) || !t.Connect {
			continue
		}
		its, err := d.dumpChecks(ctx, conn, t.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, its...)
	}
	return items, nil
}

// denyChecks は許可リスト外の操作と未知の対象 id、根の外のパスが終了状態 126 で拒否されることを確かめる。根の外のパスは、定義の files の対象の
// id で read-batch を呼び(未知の id では id の検査で拒否されて、パスの検査が壊れていても合格してしまう)、何も出さずに 126 であることを確かめる。
// files の対象が無いときは outside_path_denied の項目を出さない(確かめる対象が無い)。
func (d enrollDeps) denyChecks(ctx context.Context, conn contract.SSHConn, def checkDef) ([]selfItem, error) {
	sf := contract.CodeReceiverSelftestFailed
	denied := func(stdin []byte, argv ...string) (bool, error) {
		o, err := pullRun(ctx, d.SSH, conn, stdin, argv...)
		if err != nil {
			return false, pullErr(err)
		}
		return o.Exit == 126 && len(o.Out) == 0, nil
	}
	unl := true
	for _, argv := range [][]string{{"sitate-nosuch-op"}, {"list", "t0"}} {
		ok, err := denied(nil, argv...)
		if err != nil {
			return nil, err
		}
		unl = unl && ok
	}
	items := []selfItem{newItem("unlisted_op_denied", nil, unl, sf)}
	fid := ""
	for _, t := range def.Targets {
		if t.Files != "" {
			fid = t.ID
			break
		}
	}
	if fid == "" {
		return items, nil
	}
	out := true
	for _, p := range []string{"/etc/passwd", "../x"} {
		ok, err := denied([]byte(p+"\x00"), "read-batch", fid)
		if err != nil {
			return nil, err
		}
		out = out && ok
	}
	return append(items, newItem("outside_path_denied", nil, out, sf)), nil
}

// dumpChecks は DB の対象で dump-pg-globals と dump-pg を試行する(終了状態 0 で合格)。出力は捨てる。
func (d enrollDeps) dumpChecks(ctx context.Context, conn contract.SSHConn, id string) ([]selfItem, error) {
	sc := contract.CodeSourceSetupFailed
	var items []selfItem
	for _, x := range []struct{ op, name string }{{"dump-pg-globals", "db_globals"}, {"dump-pg", "db_dump"}} {
		o, err := pullRun(ctx, d.SSH, conn, nil, x.op, id)
		if err != nil {
			return nil, pullErr(err)
		}
		tid := id
		it := newItem(x.name, &tid, o.Exit == 0, sc)
		if !it.OK {
			lack := "tables"
			if x.name == "db_globals" {
				lack = "globals"
			}
			it.setup = &setupDetail{Step: "selftest", TargetID: &tid, Reason: "db_privileges", Lacks: []string{lack}}
		}
		items = append(items, it)
	}
	return items, nil
}
