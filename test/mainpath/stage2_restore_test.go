//go:build mainpath

package mainpath

// 第 2 段の復元と照合。復元先役のコンテナ(dest)で、root でない sitate ユーザーとして実物の sitate restore を実行する。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/format"
	"github.com/r2fish/sitate-backup/internal/keys"
	"github.com/r2fish/sitate-backup/internal/store"
)

const (
	destRestoreRoot = stateDir + "/restore-out" // sitate ユーザーの所有。--to は空のディレクトリ
	srcRoot         = "/srv/shopapp"
)

// listing は根の下を (種別 モード サイズ 更新時刻 相対パス リンク先 内容のハッシュ) の行にする。元データと復元先で同じ計算をする。
// 保存済みマニフェストは使わない。特殊ファイル(FIFO・ソケットなど)は記録に無いので種別 f・d・l だけを見る。uid・gid は復元先の実行ユーザーになるので除く。
func listing(root string) string {
	return `cd ` + shq(root) + ` && find . \( -type f -o -type d -o -type l \) -printf '%y\t%m\t%s\t%T@\t%P\t%l\n' | LC_ALL=C sort -t "$(printf '\t')" -k5,5 && ` +
		`find . -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha256sum`
}

// installCore は dest に核を置き、sitate ユーザー(rootless の podman・状態ディレクトリを持つ)を作る。bootstrap の復元先版(gate は置かない)。
func (w *s2) installCore() {
	t := w.t
	w.dst.prerequisites(t, false)
	arch := w.dst.arch()
	w.dst.put(sitateBin, readF(t, filepath.Join(w.bundle, arch, "sitate")), "0755", "root")
	w.dst.root("install -d -m 0700 -o sitate -g sitate " + destRestoreRoot)
}

// snapshotKey は世代 ref のスナップショット鍵を、バックアップサーバーの鍵(試験の合成データの鍵)と保管先のマニフェストから導く。
// 通常は restore-test(U2d)が行う、復元先へ渡す鍵の用意に当たる。台本はバックアップサーバーの master.json を管理アクセスで読み、
// 実物の KeyService で開く(master は台本の外へ出さず、試験の終了で破棄される合成の環境のもの)。
func (w *s2) snapshotKey(ref string) []byte {
	t := w.t
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	master := w.bak.root("cat " + stateDir + "/keys/master.json")
	if err := os.WriteFile(filepath.Join(dir, "keys", "master.json"), []byte(master), 0o600); err != nil {
		t.Fatal(err)
	}
	ks := &keys.KeyService{Dir: dir, Codec: &format.Codec{}, Crypto: &format.Crypto{}}
	rd := &store.ObjectStore{Endpoint: w.s3.endpoint, Bucket: w.s3.bucket, AccessKey: w.s3.rk, SecretKey: w.s3.rs, Prefix: "sitate/", Params: oneDay{}}
	// 世代の版 ID はバックアップサーバーの state.json の snapshots[] が記録している(版指定なしでは読めない)。
	var st struct {
		Snapshots []struct {
			Ref       string `json:"ref"`
			VersionID string `json:"version_id"`
		} `json:"snapshots"`
	}
	if err := json.Unmarshal([]byte(w.bak.root("cat "+stateDir+"/state.json")), &st); err != nil {
		t.Fatalf("state.json: %v", err)
	}
	ver := ""
	for _, sn := range st.Snapshots {
		if sn.Ref == ref {
			ver = sn.VersionID
		}
	}
	if ver == "" {
		t.Fatalf("state.json の snapshots[] に %s の版 ID が無い", ref)
	}
	rc, err := rd.Get(context.Background(), "manifests/"+ref, ver)
	if err != nil {
		t.Fatalf("マニフェストを読めない: %v", err)
	}
	sealed, _ := io.ReadAll(rc)
	rc.Close()
	h, err := (&format.Codec{}).Header(sealed)
	if err != nil || len(h.Mark) != 4 {
		t.Fatalf("マニフェストの見出し: %v", err)
	}
	n := uint32(h.Mark[0])<<24 | uint32(h.Mark[1])<<16 | uint32(h.Mark[2])<<8 | uint32(h.Mark[3])
	key, err := ks.OpenSnapshotKey(context.Background(), h.Wrapped, "k"+itoa(n), ref)
	if err != nil {
		t.Fatalf("スナップショット鍵を開けない: %v", err)
	}
	return key
}

func itoa(n uint32) string { b, _ := json.Marshal(n); return string(b) }

// restoreOnDest は dest で sitate restore(その場のディレクトリ。--snapshot-key-stdin)を sitate ユーザーとして実行する。
func (w *s2) restoreOnDest(ref string, to string, snap []byte) cliOut {
	line := contract.DestKeyLine{SnapshotKey: base64.StdEncoding.EncodeToString(snap)}
	line.ReadKey.KeyID, line.ReadKey.Secret = w.s3.rk, w.s3.rs
	lb, _ := json.Marshal(line)
	cmd := "sudo -n -u sitate env SITATE_TEST_TRUST_PUBKEY_FILE=" + testTrustFn + " SITATE_SIM_TIME_FILE=" + "/opt/sitate-test/shared/simtime" +
		" " + sitateBin + " restore --backup " + ref + " --to " + to + " --endpoint " + w.s3.endpoint + " --bucket " + w.s3.bucket + " --prefix sitate/ --snapshot-key-stdin"
	w.dst.holdStdin = true
	defer func() { w.dst.holdStdin = false }()
	r, err := w.dst.run(adminUser(w.t), adminSigner(w.t), cmd, append(lb, '\n'))
	if err != nil {
		w.t.Fatalf("dest で restore を実行できない: %v", err)
	}
	return parseOut(w.t, r)
}

// compareTrees は元サーバーの根と dest の復元先を同じ計算で並べて、食い違いを行ごとに返す(空なら一致)。
func (w *s2) compareTrees(restored string) []string {
	want := w.src.root(listing(srcRoot))
	got := w.dst.root(listing(restored))
	if want == got {
		return nil
	}
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	in := func(l []string) map[string]bool {
		m := map[string]bool{}
		for _, x := range l {
			m[x] = true
		}
		return m
	}
	wm, gm := in(wl), in(gl)
	var diff []string
	for _, x := range wl {
		if !gm[x] {
			diff = append(diff, "元にあり復元に無い/違う: "+x)
		}
	}
	for _, x := range gl {
		if !wm[x] {
			diff = append(diff, "復元にあり元に無い/違う: "+x)
		}
	}
	return diff
}

func (w *s2) mustMatch(label, restored string) {
	t := w.t
	if d := w.compareTrees(restored); len(d) > 0 {
		if len(d) > 20 {
			d = d[:20]
		}
		t.Fatalf("%s: 元データと復元先が一致しない(%d 行の食い違いのうち先頭):\n%s", label, len(d), strings.Join(d, "\n"))
	}
	n := bytes.Count([]byte(w.dst.root(listing(restored))), []byte("\n"))
	t.Logf("%s: 元データ(%s)と復元先の内容・モード・サイズ・更新時刻・リンク先が一致(照合の行 %d)", label, srcRoot, n)
}
