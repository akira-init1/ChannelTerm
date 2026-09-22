//go:build windows

package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
)

// defaultLayout resolves an administrator-free installation rooted in the
// current user's LOCALAPPDATA directory.
func defaultLayout() (layout, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" || !filepath.IsAbs(localAppData) {
		return layout{}, fmt.Errorf("LOCALAPPDATA does not name an absolute per-user directory")
	}
	configPath, err := config.DefaultPath()
	if err != nil {
		return layout{}, err
	}
	statePath, err := config.DefaultStatePath()
	if err != nil {
		return layout{}, err
	}
	tokenPath, err := config.DefaultHTTPAuthTokenPath()
	if err != nil {
		return layout{}, err
	}
	binDirectory := filepath.Join(localAppData, "Programs", "ChannelTerm", "bin")
	// Keep executables under the per-user Programs hierarchy while storing the
	// ownership record outside bin, so uninstalling program files cannot delete
	// the record before cleanup completes.
	return layout{
		binaryPath:   filepath.Join(binDirectory, "channelterm.exe"),
		aliasPath:    filepath.Join(binDirectory, "cterm.exe"),
		manifestPath: filepath.Join(localAppData, "ChannelTerm", "install.json"),
		pathEntry:    binDirectory,
		configPath:   configPath,
		statePath:    statePath,
		tokenPath:    tokenPath,
	}, nil
}

// pathEqual compares cleaned Windows path names case-insensitively.
func pathEqual(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}
