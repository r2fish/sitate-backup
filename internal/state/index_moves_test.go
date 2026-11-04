package state

import (
	"strings"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

func sealedFiles(t *testing.T) *StateFiles {
	t.Helper()
	s := newFiles(t)
	if err := s.StageDef(ctx, normOf(t, s, baseDef)); err != nil {
		t.Fatal(err)
	}
	if err := s.SwitchDef(ctx, contract.SwitchInput{Normalized: normOf(t, s, baseDef)}); err != nil {
		t.Fatal(err)
	}
	return s
}

// 完了条件 15: 元サーバーの所在(source.host)が変わると、その元サーバーのすべての files の id の索引が消える
// (postgresql の対象は索引を持たないので触れない)。
func TestSwitchDefSourceHostChangeDropsAllFilesIndexes(t *testing.T) {
	s := sealedFiles(t)
	mkIndex(t, s, "t1", "t2")
	moved := strings.Replace(baseDef, "host: app01.example.internal", "host: app02.example.internal", 1)
	if err := s.SwitchDef(ctx, contract.SwitchInput{Normalized: normOf(t, s, moved)}); err != nil {
		t.Fatal(err)
	}
	if hasIndex(s, "t1") || hasIndex(s, "t2") {
		t.Errorf("所在が変わったのに索引が残った: t1=%v t2=%v", hasIndex(s, "t1"), hasIndex(s, "t2"))
	}
}

// 完了条件 15: host_key_fp が null の要素(作成 / 破棄の組の restore-change)は比べず、host_keys[] を更新せず、索引も消さない。
func TestSwitchDefNullHostKeyFPIsNeitherComparedNorWritten(t *testing.T) {
	s := sealedFiles(t)
	pin := []contract.HostKeyUpdate{{Role: "source", Host: "app01.example.internal", HostKeyFP: strp("SHA256:aaa")}}
	if err := s.SwitchDef(ctx, contract.SwitchInput{Normalized: normOf(t, s, baseDef), HostKeys: pin}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.readState()
	mkIndex(t, s, "t1", "t2")
	null := []contract.HostKeyUpdate{{Role: "source", Host: "app01.example.internal", HostKeyFP: nil}}
	if err := s.SwitchDef(ctx, contract.SwitchInput{Normalized: normOf(t, s, baseDef), HostKeys: null}); err != nil {
		t.Fatal(err)
	}
	after, _ := s.readState()
	if string(after["host_keys"]) != string(before["host_keys"]) || !strings.Contains(string(after["host_keys"]), "SHA256:aaa") {
		t.Errorf("host_keys が変わった: %s -> %s", before["host_keys"], after["host_keys"])
	}
	if !hasIndex(s, "t1") || !hasIndex(s, "t2") {
		t.Error("null の指紋で索引が消えた")
	}
}

func strp(s string) *string { return &s }
