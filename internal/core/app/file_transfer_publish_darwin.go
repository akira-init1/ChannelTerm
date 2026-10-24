package app

import "golang.org/x/sys/unix"

// RENAME_EXCL preserves a destination created after preflight.
func publishTransferPath(source, destination string) error {
	return unix.RenamexNp(source, destination, unix.RENAME_EXCL)
}
