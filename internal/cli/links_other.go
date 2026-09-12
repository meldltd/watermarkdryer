//go:build !unix

package cli

import "io/fs"

func hasMultipleLinks(info fs.FileInfo) bool { return false }
