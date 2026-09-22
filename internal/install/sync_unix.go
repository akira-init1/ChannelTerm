//go:build linux || darwin

package install

import "os"

// syncParentDirectory makes a preceding rename durable across a system crash
// on filesystems that honor directory fsync.
func syncParentDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
