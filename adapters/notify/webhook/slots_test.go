package main

// 主経路の第 2 段で見つけた誤り E1 の試験: adapter.json の secrets[] は、アダプタが実際に読む枠(main.go の url)を宣言する。
// 核は宣言に無い枠を要求から落とす(spec 3.2.1。internal/adapter/request.go)ので、空のままでは Webhook の URL が届かない。

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestAdapterJSONDeclaresTheSlotsTheAdapterReads(t *testing.T) {
	b, err := os.ReadFile("adapter.json")
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Secrets []string `json:"secrets"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if want := []string{"url"}; !reflect.DeepEqual(d.Secrets, want) {
		t.Fatalf("secrets[] = %v, 読む枠は %v", d.Secrets, want)
	}
}
