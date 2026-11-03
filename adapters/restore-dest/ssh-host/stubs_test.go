package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 偽のホストの PATH の先頭に置く試験用の実行ファイル。実物の podman・gocryptfs は使わない。
// podman: $HOME/.pod に "<id> <label|->" の行で容器を持つ。$HOME/.podmode が fail なら rc 125、noise なら標準出力に雑音だけを出す、
// stuck なら rm が何も消さない。rm の呼び出しは $HOME/.podrm に残る。
// gocryptfs: -init は成功し、マウントは失敗する。mkdir: $HOME/.mkfail があれば */cipher の作成だけ失敗する。
const podmanStub = `#!/bin/bash
m=$(cat "$HOME/.podmode" 2>/dev/null)
[ "$m" = fail ] && { echo "Error: stub" >&2; exit 125; }
[ "$m" = noise ] && { echo "/etc/containers/storage.conf"; exit 0; }
f="$HOME/.pod"; touch "$f"
filt=""; fmt=0
for a in "$@"; do case "$a" in label=*) filt="${a#label=}";; '{{'*) fmt=1;; esac; done
case "$1" in
ps)
  while read -r id lab; do
    [ -n "$id" ] || continue
    case "$filt" in
      sitate-run=*) [ "$lab" = "${filt#sitate-run=}" ] || continue;;
      sitate-run) [ "$lab" != "-" ] || continue;;
    esac
    if [ $fmt = 1 ]; then [ "$lab" = - ] && lab=""; echo "$id $lab"; else echo "$id"; fi
  done < "$f";;
rm)
  id="${@: -1}"; echo "$id" >> "$HOME/.podrm"
  if [ "$m" != stuck ]; then grep -v "^$id " "$f" > "$f.n"; mv "$f.n" "$f"; fi;;
esac
exit 0
`

const gocryptfsStub = `#!/bin/bash
for a in "$@"; do [ "$a" = "-init" ] && { cat >/dev/null; exit 0; }; done
cat >/dev/null; exit 1
`

const mkdirStub = `#!/bin/bash
case "$*" in *cipher*) [ -e "$HOME/.mkfail" ] && exit 1;; esac
exec /bin/mkdir "$@"
`

// ps: 偽の宛先は試験者と同じ uid で動くため、`ps -u <uid>` の一覧に無関係な処理(並行して走る別のパッケージの試験など)が載り、
// stop の remaining に拾われる。製品の台本は弱めず、偽のホストの ps だけを、この試験のプロセス(SITATE_TEST_PID)の子孫に絞る。
// 本物の宛先では専用ユーザーの処理だけが載る。それ以外の形の呼び出しは本物の ps へそのまま渡す。
const psStub = `#!/bin/bash
real=/usr/bin/ps; [ -x "$real" ] || real=/bin/ps
case "$1" in
-u)
  [ -n "${SITATE_TEST_PID:-}" ] || exec "$real" "$@"
  "$real" "$@" | awk -v root="$SITATE_TEST_PID" '{pid[NR]=$1; par[$1]=$2; line[NR]=$0; n=NR}
    END{for(i=1;i<=n;i++){q=pid[i]; ok=0; while(q!=""&&q>1){if(q==root){ok=1;break}; q=par[q]}; if(ok)print line[i]}}'
  exit ${PIPESTATUS[0]};;
esac
exec "$real" "$@"
`

func stubBin(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	for n, body := range map[string]string{"podman": podmanStub, "gocryptfs": gocryptfsStub, "mkdir": mkdirStub, "ps": psStub} {
		if err := os.WriteFile(filepath.Join(d, n), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// setHome は偽ホストの HOME の下のファイルを置き換える。
func (f *fakeSSH) setHome(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.home, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeSSH) readHome(name string) string {
	b, _ := os.ReadFile(filepath.Join(f.home, name))
	return string(b)
}
