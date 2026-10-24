package app

import "golang.org/x/sys/windows"

// MoveFileEx without REPLACE_EXISTING refuses an existing destination. Paths
// remain on one volume because staging is created beside the final target.
func publishTransferPath(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}
