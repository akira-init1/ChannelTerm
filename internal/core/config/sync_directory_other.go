//go:build !windows

package config

import "os"

// syncDirectory persists the renamed directory entry on platforms that permit
// opening and synchronizing a directory descriptor.
func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
