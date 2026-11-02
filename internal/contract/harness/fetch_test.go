package harness

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/r2fish/sitate-backup/internal/contract"
)

// 断片の長さの上限の境界: 最大長ちょうどは受理し、1 バイト超えは本体を読む前に拒否する。
func TestFetchFragmentMaxBoundary(t *testing.T) {
	var v struct {
		FragmentMax int `json:"fragment_max"`
	}
	LoadVectors(t, "cli/fetch.json", &v)
	if v.FragmentMax != contract.FetchFragmentMax {
		t.Errorf("ベクトルの fragment_max %d が定数 %d と違う", v.FragmentMax, contract.FetchFragmentMax)
	}
	output := func(size uint32, bodyLen int) []byte {
		b := []byte(`{"ok":true}` + "\n")
		b = binary.BigEndian.AppendUint32(b, size)
		b = append(b, make([]byte, bodyLen)...)
		return binary.BigEndian.AppendUint32(b, 0)
	}
	_, body, err := ReadFetch(bytes.NewReader(output(contract.FetchFragmentMax, contract.FetchFragmentMax)))
	if err != nil || len(body) != contract.FetchFragmentMax {
		t.Errorf("最大長ちょうど: %v 本体 %d バイト", err, len(body))
	}
	if _, _, err := ReadFetch(bytes.NewReader(output(contract.FetchFragmentMax+1, 0))); err == nil {
		t.Error("最大長を超える断片を受理した")
	}
}

// sitate.fetch/v1 の試験ベクトル(testdata/cli/fetch.json)を読み手に通す。アプリ側の読み手も同じベクトルで照らす。
func TestFetchVectors(t *testing.T) {
	var v struct {
		Valid []struct {
			Name    string `json:"name"`
			Hex     string `json:"hex"`
			BodyHex string `json:"body_hex"`
		} `json:"valid"`
		Invalid []struct{ Name, Hex string } `json:"invalid"`
	}
	LoadVectors(t, "cli/fetch.json", &v)
	if len(v.Valid) == 0 || len(v.Invalid) == 0 {
		t.Fatal("試験ベクトルが空")
	}
	for _, c := range v.Valid {
		raw, _ := hex.DecodeString(c.Hex)
		want, _ := hex.DecodeString(c.BodyHex)
		env, body, err := ReadFetch(bytes.NewReader(raw))
		if err != nil || !bytes.Equal(body, want) || len(env) == 0 {
			t.Errorf("%s: %v 本体 %x 期待 %x", c.Name, err, body, want)
		}
	}
	for _, c := range v.Invalid {
		raw, _ := hex.DecodeString(c.Hex)
		if _, _, err := ReadFetch(bytes.NewReader(raw)); err == nil {
			t.Errorf("%s: 不正な出力を受理した", c.Name)
		}
	}
}
