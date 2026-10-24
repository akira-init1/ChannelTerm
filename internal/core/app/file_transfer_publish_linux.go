package app

import "golang.org/x/sys/unix"

// Exclusive same-filesystem publication rejects even a racing destination.
// Unsupported kernels/filesystems fail closed; there is no replacing fallback.
func publishTransferPath(source, destination string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
}
