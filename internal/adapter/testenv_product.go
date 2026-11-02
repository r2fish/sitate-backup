//go:build !sitate_testbuild

package adapter

// testBuildEnv は製品ビルドでは子に何も足さない(試験の切り替えと追加の CA の変数は試験ビルドだけ。spec 3.2.1)。
func testBuildEnv() map[string]string { return nil }
