//go:build windows

package install

func syncParentDirectory(string) error {
	// Windows directory handles do not provide the portable Sync operation used
	// by Unix. MoveFileEx(..., MOVEFILE_WRITE_THROUGH) supplies write-through for
	// replacements instead.
	return nil
}
