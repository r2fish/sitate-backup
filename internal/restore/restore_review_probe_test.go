package restore

// F8(隔離の検査の台本)と F4・F15(鍵 ID は見出しだけで決める)の試験。

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/contract/fake"
	"github.com/r2fish/sitate-backup/internal/contract/harness"
)

// probeRun は台本を sh で実行する。PATH は binDir だけ(道具の欠けを再現する)。stubs は道具名 → sh の本体。
func probeRun(t *testing.T, port int, stubs map[string]string) contract.DestProbeOut {
	t.Helper()
	bin := t.TempDir()
	for _, tool := range []string{"uname", "awk", "grep", "hostname", "cat"} {
		if p, err := exec.LookPath(tool); err == nil {
			_ = os.Symlink(p, filepath.Join(bin, tool))
		}
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sh, _ := exec.LookPath("sh")
	cmd := exec.Command(sh, "-s")
	cmd.Env = []string{"PATH=" + bin}
	cmd.Stdin = bytes.NewReader(probeScript([]string{"prod.example.invalid"}, port))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("台本が失敗: %v", err)
	}
	var po contract.DestProbeOut
	if json.Unmarshal(out, &po) != nil {
		t.Fatalf("出力が JSON でない: %s", out)
	}
	return po
}

func probeOK(po contract.DestProbeOut) map[string]bool {
	m := map[string]bool{}
	for _, p := range po.Isolation {
		m[p.Name] = p.OK
	}
	return m
}

const ssHeader = `echo "State Recv-Q Send-Q Local:Port Peer:Port"; echo "LISTEN 0 128 0.0.0.0:%PORT% 0.0.0.0:*"; echo "LISTEN 0 128 127.0.0.1:5432 0.0.0.0:*"`

func TestProbeScriptToolsMissingFail(t *testing.T) {
	m := probeOK(probeRun(t, 22, nil))
	if m["inbound"] || m["outbound"] || !m["dns_name"] {
		t.Errorf("ss・timeout・bash が無いのに合格した: %v", m)
	}
}

func TestProbeScriptPortAndOutbound(t *testing.T) {
	ss := func(port string) string { return strings.ReplaceAll(ssHeader, "%PORT%", port) }
	stubs := func(port, timeoutRC string) map[string]string {
		return map[string]string{"ss": ss(port), "timeout": "exit " + timeoutRC, "bash": "exit 1"}
	}
	if m := probeOK(probeRun(t, 2222, stubs("2222", "124"))); !m["inbound"] || !m["outbound"] {
		t.Errorf("dest.at のポート 2222 の待ち受けと時限切れの接続は合格のはず: %v", m)
	}
	if m := probeOK(probeRun(t, 22, stubs("2222", "124"))); m["inbound"] {
		t.Errorf("ポート 22 を許すとき 2222 の待ち受けは不合格のはず: %v", m)
	}
	if m := probeOK(probeRun(t, 22, stubs("22", "0"))); m["outbound"] {
		t.Errorf("接続が通った(終了 0)のに合格した: %v", m)
	}
	for _, rc := range []string{"126", "127"} {
		if m := probeOK(probeRun(t, 22, stubs("22", rc))); m["outbound"] {
			t.Errorf("道具の欠け(%s)を合格にした: %v", rc, m)
		}
	}
}

// markCodec はマニフェストの見出しの印(鍵 ID)を、本体の先頭の "TAGK<n>" から返す Codec(偽の Codec は印を持たない)。
type markCodec struct{ fake.Codec }

func (markCodec) Header(b []byte) (contract.Header, error) {
	if s := string(b); strings.HasPrefix(s, "TAGK") {
		n := byte(s[4] - '0')
		return contract.Header{FormatVersion: 1, Mark: []byte{0, 0, 0, n}}, nil
	}
	return fake.Codec{}.Header(b)
}

func (c markCodec) Decode(kind, id string, sealed, key []byte) ([]byte, error) {
	return c.Codec.Decode(kind, id, bytes.TrimPrefix(sealed, []byte("TAGK9")), key)
}

// tagStore は ref のマニフェストを "TAGK9" 付きで返し、読んだバイト数を数える。
type tagStore struct {
	contract.ObjectStore
	ref  string
	read int
}

type countRC struct {
	io.ReadCloser
	n *int
}

func (c countRC) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	*c.n += n
	return n, err
}

func (s *tagStore) Get(c context.Context, key, ver string) (io.ReadCloser, error) {
	rc, err := s.ObjectStore.Get(c, key, ver)
	if err != nil || key != "manifests/"+s.ref {
		return rc, err
	}
	return countRC{io.NopCloser(io.MultiReader(strings.NewReader("TAGK9"), rc, bytes.NewReader(bytes.Repeat([]byte("x"), 4096)))), &s.read}, nil
}

// noKeyS はキーリングに無い鍵(k9)のスナップショット鍵を開けない(実物の KeyService と同じ)。
type noKeyS struct{ fake.KeyService }

func (noKeyS) OpenSnapshotKey(c context.Context, w []byte, id, ref string) ([]byte, error) {
	if id == "k9" {
		return nil, &contract.Error{Code: contract.CodeDecryptFailed, Class: "verification", Detail: json.RawMessage(`{}`), Next: []contract.Next{}}
	}
	return fake.KeyService{}.OpenSnapshotKey(c, w, id, ref)
}

// F4・F15: 鍵 ID は見出しの印だけで決める。キーリングに無い鍵で復号できない世代は KEYRING_MISMATCH(key_id_missing)で、本体は読まない(見出しの分だけ)。
func TestKeyringHeaderOnlyDetectsUnopenableGeneration(t *testing.T) {
	e := rtWorld(t)
	e.Restore.Codec, e.Restore.KeyS = markCodec{}, noKeyS{}
	ts := &tagStore{ObjectStore: e.Restore.Store, ref: refB}
	e.Restore.Store = ts
	_, err := e.RestoreTest(ctx, rtCall(false))
	ce := harness.ExpectCode(t, err, contract.CodeKeyringMismatch)
	var d struct{ Reason string }
	_ = json.Unmarshal(ce.Detail, &d)
	if d.Reason != "key_id_missing" {
		t.Errorf("detail: %s", ce.Detail)
	}
	if ts.read == 0 || ts.read > 256+len("TAGK9") {
		t.Errorf("見出しだけを読むはず: %d バイト", ts.read)
	}
}
