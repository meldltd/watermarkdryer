//go:build unix

package cli

import (
	"io/fs"
	"syscall"
)

func hasMultipleLinks(info fs.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Nlink > 1
}
