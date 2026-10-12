//go:build linux

package backup

import "golang.org/x/sys/unix"

// renameNoReplace publishes a file or directory atomically while refusing to
// replace any object that appeared at the destination after the caller's
// initial validation.
func renameNoReplace(oldPath, newPath string) error {
	return unix.Renameat2(
		unix.AT_FDCWD,
		oldPath,
		unix.AT_FDCWD,
		newPath,
		unix.RENAME_NOREPLACE,
	)
}
