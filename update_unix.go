//go:build !windows

package main

import "os"

// installBinary replaces dst with src. On Unix a running process keeps its
// open inode, so renaming over the in-use binary works and is atomic on the
// same filesystem.
func installBinary(src, dst string) error {
	return os.Rename(src, dst)
}
