//go:build !linux

package cli

import (
	"errors"
	"os"
)

func fsTypeOf(string) (string, error) { return "", errors.New("unsupported os") }

func fileOwner(os.FileInfo) int { return -1 }

func diskEncrypted(string) bool { return false }
