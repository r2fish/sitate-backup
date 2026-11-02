package cli

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

// statfs の f_type(linux/magic.h)から ext4・xfs などの名前へ。未知の値は 0x<16 進> の文字列にする(状態ディレクトリを置けない種類だけ名前が要る)。
var fsMagic = map[uint32]string{
	0xEF53: "ext4", 0x58465342: "xfs", 0x9123683E: "btrfs", 0x01021994: "tmpfs", 0x6969: "nfs",
	0xFF534D42: "cifs", 0xFE534D42: "smb2", 0x517B: "smb", 0x65735546: "fuse", 0xF2F52010: "f2fs",
}

func fsTypeOf(path string) (string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return "", err
	}
	t := uint32(st.Type)
	if n, ok := fsMagic[t]; ok {
		return n, nil
	}
	return fmt.Sprintf("0x%x", t), nil
}

func fileOwner(fi os.FileInfo) int {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid)
	}
	return -1
}

// diskEncrypted は path を載せるブロックデバイスが dm-crypt(device-mapper の uuid が CRYPT- で始まる)の上にあるか。
func diskEncrypted(path string) bool {
	var st syscall.Stat_t
	if syscall.Stat(path, &st) != nil {
		return false
	}
	dev := uint64(st.Dev)
	major := (dev>>8)&0xfff | (dev>>32)&^0xfff
	minor := dev&0xff | (dev>>12)&^0xff
	b, err := os.ReadFile(fmt.Sprintf("/sys/dev/block/%d:%d/dm/uuid", major, minor))
	if err != nil { // dm でないデバイスは uuid が無い。判定できなければ偽(advisory)
		return false
	}
	return strings.HasPrefix(string(b), "CRYPT-")
}
