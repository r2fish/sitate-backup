package cli

// 結線(作業指示書 5.17 の完了条件 24): 受け入れ済みの部品の実物を Deps に組む。internal/cli で部品のパッケージを import してよいのは、
// wire_*.go だけ。未受け入れの部品(U2b・U2d・U2e・U2g・U5 などのアダプタ)の入口は、アダプタが置かれていなければ実行器が
// NOT_IMPLEMENTED 相当で返し、ここでは偽物を入れない。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/r2fish/sitate-backup/internal/acquire"
	"github.com/r2fish/sitate-backup/internal/adapter"
	"github.com/r2fish/sitate-backup/internal/approval"
	"github.com/r2fish/sitate-backup/internal/contract"
	"github.com/r2fish/sitate-backup/internal/format"
	"github.com/r2fish/sitate-backup/internal/keys"
	"github.com/r2fish/sitate-backup/internal/restore"
	"github.com/r2fish/sitate-backup/internal/state"
	"github.com/r2fish/sitate-backup/internal/store"
)

// AdaptersDirDefault はアダプタの置き場(spec 3.2.1。<置き場>/<kind>/<name>/)。
const AdaptersDirDefault = "/usr/libexec/sitate/adapters"

// ProvisionNameDefault は保管先準備アダプタの既定の名前(--adapter が優先)。
const ProvisionNameDefault = "b2"

func init() { recvHelper = acquire.RunRecvHelper }

func hostArch() string {
	if runtime.GOARCH == "arm64" {
		return "linux-arm64"
	}
	return "linux-x86_64"
}

// wireParts は Deps の継ぎ目(Clock・Secrets・State・SSH ほか。DefaultDeps が先に組む)の上に、部品の実物を組む。
func wireParts(d *Deps, stateDir string) {
	d.Codec, d.Crypto = &format.Codec{}, &format.Crypto{}
	write, read := newDynStore(d, "write"), newDynStore(d, "read")
	d.Object = write
	ver := &keys.Verifier{}
	d.Adapters = &adapter.Runner{Root: AdaptersDirDefault, Version: Version, Arch: hostArch(), Verifier: ver, Stderr: adapterStderrSink(stateDir)}
	d.KeyS = &keys.KeyService{Dir: stateDir, Codec: d.Codec, Crypto: d.Crypto, Provision: d.Adapters, ProvisionName: ProvisionNameDefault}
	n := &state.Notifier{State: d.State, Adapters: d.Adapters, Clock: d.Clock, Secrets: d.Secrets}
	d.Notifier = n
	d.TestNotifier = &state.TestNotifier{State: d.State, Adapters: d.Adapters, Clock: d.Clock, Secrets: d.Secrets, Notifier: n}
	d.Recorder = &state.Recorder{State: d.State, Store: write, Codec: d.Codec, KeyS: d.KeyS, Notifier: n, Clock: d.Clock, Dir: stateDir}
	d.Timers = &state.Timers{Dir: TimersDirDefault(), Scheduler: state.DetectScheduler(""), State: d.State, Run: runCommand}
	idx := &acquire.Index{Dir: stateDir, Codec: d.Codec, KeyS: d.KeyS, Store: read}
	d.Index, d.CommitIndex = idx, idx.Commit
	var off contract.Offsetter
	if o, ok := d.Clock.(contract.Offsetter); ok {
		off = o
	}
	d.Store = &store.Retention{Store: write, State: d.State, Codec: d.Codec, KeyS: d.KeyS, Clock: d.Clock, Offset: off, Index: idx,
		Intents: d.writeIntents, Params: d.Retention, BinVersion: Version}
	wireAcquire(d, stateDir, write, idx)
	wireKeysRestore(d, stateDir, write, read, ver)
}

func wireAcquire(d *Deps, stateDir string, write *dynStore, idx *acquire.Index) {
	exe, _ := os.Executable()
	acq := &acquire.Acquire{Store: write, State: d.State, Index: idx, Codec: d.Codec, KeyS: d.KeyS, Adapters: d.Adapters, Clock: d.Clock, SSH: d.SSH,
		Secrets: d.Secrets, Dir: stateDir, WorkDir: filepath.Join(stateDir, "work"), Container: &acquire.Container{RecvExe: exe}, Retention: d.Retention}
	d.Acquire = acq
	d.Ingest = func(ctx context.Context, c contract.Call) (IngestOut, error) { return ingestOut(ctx, acq, idx, c) }
	d.RunCleanup = append(d.RunCleanup, &acquire.RunCleanup{})
}

func wireKeysRestore(d *Deps, stateDir string, write, read *dynStore, ver *keys.Verifier) {
	probe := newDynProbe(write, read)
	d.Keys = &keys.Keys{Store: write, Probe: probe, State: d.State, KeyS: d.KeyS, Codec: d.Codec, Provision: d.Adapters, Clock: d.Clock, Secrets: d.Secrets,
		Dir: stateDir, Recorder: d.Recorder, Verifier: ver, Bundle: d.Bundles, OS: d.OSProbe, Timers: d.Timers, ProvisionName: ProvisionNameDefault}
	d.Restore = &restore.Restore{Store: read, State: d.State, Adapters: d.Adapters, Codec: d.Codec, KeyS: d.KeyS, Crypto: d.Crypto, Recorder: d.Recorder,
		Clock: d.Clock, SSH: d.SSH, Bundle: d.Bundles, Secrets: d.Secrets, OpenStore: d.StoreOpener, Retention: d.Store}
	d.RunCleanup = append(d.RunCleanup, &restore.RunCleanup{State: d.State, Adapters: d.Adapters, Secrets: d.Secrets})
	src := &acquire.SourceEnroller{SSH: d.SSH, Secrets: d.Secrets, State: d.State}
	dst := &restore.RestoreDestEnroller{Store: read, State: d.State, Adapters: d.Adapters, Codec: d.Codec, KeyS: d.KeyS, Crypto: d.Crypto, Recorder: d.Recorder,
		Clock: d.Clock, Secrets: d.Secrets}
	d.Approval = &approval.Approval{State: d.State, Enroller: kindEnroller{source: src, dest: dst}, Recorder: d.Recorder, Clock: approvalClock{d: d},
		TestNotifier: d.TestNotifier, Dir: stateDir, Planner: d.Planner, ReadDefinition: d.ReadDefinition, Surveyor: &acquire.Surveyor{SSH: d.SSH}}
}

// kindEnroller は登録の種別(source / restore-dest)で実物を選ぶ(approval の Enroller は 1 つ)。
type kindEnroller struct{ source, dest contract.Enroller }

func (k kindEnroller) Enroll(ctx context.Context, in contract.EnrollInput) (contract.EnrollResult, error) {
	if in.Kind == "restore-dest" {
		return k.dest.Enroll(ctx, in)
	}
	return k.source.Enroll(ctx, in)
}

// writeIntents は実行の記録の write_intent の keys[](store.Retention.Intents)。
func (d *Deps) writeIntents(ctx context.Context) ([]string, error) {
	r, ok := d.Recorder.(journalReader)
	if !ok {
		return nil, nil
	}
	items, err := r.Entries(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, it := range items {
		if it.Kind != "write_intent" {
			continue
		}
		var b struct {
			Keys []string `json:"keys"`
		}
		if json.Unmarshal(it.Body, &b) == nil {
			out = append(out, b.Keys...)
		}
	}
	return out, nil
}

// adapterStderrSink はアダプタの標準エラー(伏せ字済み)を <状態ディレクトリ>/adapter-stderr.log に足す(モード 0600)。書けなければ捨てる。
func adapterStderrSink(stateDir string) func(adapter.StderrRecord) {
	return func(r adapter.StderrRecord) {
		f, err := os.OpenFile(filepath.Join(stateDir, "adapter-stderr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer f.Close()
		b, _ := json.Marshal(map[string]any{"run_id": r.RunID, "kind": r.Kind, "name": r.Name, "text": r.Text, "dropped": r.Dropped})
		_, _ = f.Write(append(b, '\n'))
	}
}
