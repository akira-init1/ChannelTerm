//go:build linux || darwin

package app

import (
	"fmt"
	"math"

	"golang.org/x/sys/unix"
)

func localTransferStorage(path string) (transferStorage, error) {
	path, err := existingStoragePath(path)
	if err != nil {
		return transferStorage{}, err
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return transferStorage{}, err
	}
	block := int64(stat.Bsize)
	if block <= 0 || uint64(stat.Bavail) > uint64(math.MaxInt64/block) {
		return transferStorage{}, ErrTransferSpaceUnavailable
	}
	inodes := int64(-1)
	if stat.Files > 0 && uint64(stat.Ffree) <= math.MaxInt64 {
		inodes = int64(stat.Ffree)
	}
	return transferStorage{available: int64(stat.Bavail) * block, block: block, inodes: inodes, volume: fmt.Sprint(stat.Fsid)}, nil
}
