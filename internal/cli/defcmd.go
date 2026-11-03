package cli

// def validate・def show(spec 3.1.2。D-0095、D-0101)。定義の検査と正規化は StateFiles.ValidateDef、候補の保存は StageDef。

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"sort"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func init() {
	routeOwn["def validate"] = defValidate
	routeOwn["def show"] = defShow
}

// stdinReader は標準入力の共有の読み手。--secrets-stdin の 1 行目の後に --file - の定義を続けて読めるよう、buildCall と同じ読み手を使う。
func (d *Deps) stdinReader() *bufio.Reader {
	if d.stdinBuf == nil && d.Stdin != nil {
		d.stdinBuf = bufio.NewReader(d.Stdin)
	}
	return d.stdinBuf
}

func defValidate(d *Deps, ctx context.Context, c contract.Call) (contract.Reply, error) {
	if d.State == nil {
		return notImpl("def-validate")
	}
	src := ""
	if v := c.Args["file"]; len(v) > 0 {
		src = v[0]
	}
	var in []byte
	var err error
	if src == "-" {
		r := d.stdinReader()
		if r == nil {
			return contract.Reply{}, argErr("--file", "missing")
		}
		in, err = io.ReadAll(r)
	} else {
		in, err = os.ReadFile(src)
	}
	if err != nil {
		return contract.Reply{}, argErr("--file", "format")
	}
	cur, _, derr := d.State.ReadDef(ctx)
	approved := derr == nil
	if derr != nil {
		if e, ok := derr.(*contract.Error); !ok || (e.Code != contract.CodeNotSealed && e.Code != contract.CodeNotInitialized) {
			return contract.Reply{}, derr
		}
	}
	norm, hash, issues, err := d.State.ValidateDef(ctx, in, false)
	if err != nil {
		return contract.Reply{}, err
	}
	pending := []string{}
	if norm == nil && !approved {
		// seal の前は、導入の後の手順で置かれる参照名(storage[0].key_ref、restore.dest.key_ref)の未解決だけを許す(D-0160)。
		var ok bool
		if pending, ok = onlyPendingRefs(issues); ok {
			if norm, hash, issues, err = d.State.ValidateDef(ctx, in, true); err != nil {
				return contract.Reply{}, err
			}
		}
	}
	if norm == nil {
		return contract.Reply{}, newErr(contract.CodeDefInvalid, map[string]any{"issues": issues})
	}
	if !approved && !c.DryRun { // seal の前は通った最新の内容を候補として保存する(--dry-run は保存しない)
		if err := d.State.StageDef(ctx, norm); err != nil {
			return contract.Reply{}, err
		}
	}
	var diff any
	if approved {
		diff = defDiff(cur, norm)
	}
	b, _ := json.Marshal(map[string]any{"normalized": json.RawMessage(norm), "def_hash": hash, "diff_from_approved": diff, "pending_refs": pending})
	return contract.Reply{Result: b}, nil
}

// pendingRefPointers は seal の前の def validate が未解決のまま通す参照名の JSON Pointer(spec 3.1.2 の def validate。D-0160)。
var pendingRefPointers = map[string]bool{"/storage/0/key_ref": true, "/restore/dest/key_ref": true}

// onlyPendingRefs は issues が、未解決のまま通してよい参照名の指摘(unresolved_ref)だけで、1 件以上あるときにその Pointer の列(昇順)を返す。
func onlyPendingRefs(issues json.RawMessage) ([]string, bool) {
	var l []struct{ Pointer, Reason string }
	if json.Unmarshal(issues, &l) != nil || len(l) == 0 {
		return nil, false
	}
	out := []string{}
	for _, i := range l {
		if i.Reason != "unresolved_ref" || !pendingRefPointers[i.Pointer] {
			return nil, false
		}
		out = append(out, i.Pointer)
	}
	sort.Strings(out)
	return out, true
}

func defShow(d *Deps, ctx context.Context, _ contract.Call) (contract.Reply, error) {
	if d.State == nil {
		return notImpl("def-show")
	}
	norm, hash, err := d.State.ReadDef(ctx)
	sealed := err == nil
	var approvedAt any
	if err != nil {
		if e, ok := err.(*contract.Error); !ok || (e.Code != contract.CodeNotSealed && e.Code != contract.CodeNotInitialized) {
			return contract.Reply{}, err
		}
		var found bool
		if norm, hash, found, err = d.State.ReadCandidate(ctx); err != nil {
			return contract.Reply{}, err
		}
		if !found {
			return contract.Reply{}, newErr(contract.CodeDefInvalid, map[string]any{"issues": []any{}})
		}
	} else {
		var ad struct {
			ApprovedAt *string `json:"approved_at"`
		}
		if v, gerr := d.State.Get(ctx, "approved_def"); gerr == nil {
			_ = json.Unmarshal(v, &ad)
		}
		if ad.ApprovedAt != nil {
			approvedAt = *ad.ApprovedAt
		}
	}
	// storage_country は価格表(data/prices)が要る。cli は価格表を読まないので unknown を返す(報告の「仕様の穴」参照)。
	var def struct {
		Storage []struct {
			Endpoint string `json:"endpoint"`
		} `json:"storage"`
	}
	_ = json.Unmarshal(norm, &def)
	countries := []map[string]any{}
	for _, s := range def.Storage {
		countries = append(countries, map[string]any{"endpoint": s.Endpoint, "status": "unknown", "country": nil})
	}
	b, _ := json.Marshal(map[string]any{"normalized": json.RawMessage(norm), "def_hash": hash, "sealed": sealed, "approved_at": approvedAt, "storage_country": countries})
	return contract.Reply{Result: b}, nil
}

type diffItem struct {
	Pointer string `json:"pointer"`
	Before  any    `json:"before"`
	After   any    `json:"after"`
}

// defDiff は承認済みの定義と新しい定義の差を JSON Pointer ごとに返す(オブジェクトは再帰、配列と値は 1 か所として比べる)。
func defDiff(oldN, newN []byte) []diffItem {
	var a, b any
	_ = json.Unmarshal(oldN, &a)
	_ = json.Unmarshal(newN, &b)
	out := []diffItem{}
	walkDiff("", a, b, &out)
	return out
}

func walkDiff(ptr string, a, b any, out *[]diffItem) {
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if !aok || !bok {
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		if string(x) != string(y) {
			*out = append(*out, diffItem{ptr, a, b})
		}
		return
	}
	keys := map[string]bool{}
	for k := range am {
		keys[k] = true
	}
	for k := range bm {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		walkDiff(ptr+"/"+escPtr(k), am[k], bm[k], out)
	}
}

func escPtr(k string) string {
	r := make([]byte, 0, len(k))
	for i := 0; i < len(k); i++ {
		switch k[i] {
		case '~':
			r = append(r, '~', '0')
		case '/':
			r = append(r, '~', '1')
		default:
			r = append(r, k[i])
		}
	}
	return string(r)
}
