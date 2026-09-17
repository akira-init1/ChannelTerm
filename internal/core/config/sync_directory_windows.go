package config

// syncDirectory is a no-op on Windows because Go cannot open a directory with
// the access needed by File.Sync. The temporary file itself is synchronized
// before the atomic replacement.
func syncDirectory(string) error {
	return nil
}
