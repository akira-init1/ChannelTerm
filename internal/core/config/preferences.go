package config

import (
	"errors"
	"os"
)

// Preferences contains local client preferences, independent of connection
// profiles, Host transfer grants, and Transport construction.
type Preferences struct {
	FileTransfer TransferPreferences `toml:"file_transfer,omitempty"`
}

// TransferPreferences controls how local clients select directory transfers.
// An omitted DirectoryMode means ask; it does not authorize a transfer.
type TransferPreferences struct {
	DirectoryMode string `toml:"directory_mode,omitempty"`
}

// Mode resolves the omitted preference and rejects unsupported values.
func (p TransferPreferences) Mode() (string, error) {
	switch p.DirectoryMode {
	case "", "ask":
		return "ask", nil
	case "tar", "files":
		return p.DirectoryMode, nil
	default:
		return "", errors.New("preferences.file_transfer.directory_mode must be ask, tar or files")
	}
}

// LoadTransferPreferences reads a local client preference without creating a
// file. Empty path selects the platform default; missing files resolve to ask.
func LoadTransferPreferences(path string) (TransferPreferences, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return TransferPreferences{}, err
		}
	}
	file, err := Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return TransferPreferences{}, nil
	}
	return file.Preferences.FileTransfer, err
}

// DefaultPreferences returns the default user preferences.
//
// Zero values preserve omitted tables in older configuration files.
func DefaultPreferences() Preferences {
	return Preferences{}
}
