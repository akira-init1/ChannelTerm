//go:build linux || darwin

package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
)

// defaultLayout resolves per-user command, state, configuration, and credential
// locations according to the host Unix platform.
func defaultLayout() (layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return layout{}, fmt.Errorf("resolve user home directory: %w", err)
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
	binDirectory := filepath.Join(home, ".local", "bin")
	// macOS keeps the ownership record with application support data. Linux
	// follows XDG_STATE_HOME because the manifest is mutable installer state,
	// not user-authored configuration.
	manifestPath := filepath.Join(filepath.Dir(configPath), "install.json")
	if runtime.GOOS == "linux" {
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" || !filepath.IsAbs(stateHome) {
			stateHome = filepath.Join(home, ".local", "state")
		}
		manifestPath = filepath.Join(stateHome, "channelterm", "install.json")
	}
	return layout{
		binaryPath:   filepath.Join(binDirectory, "channelterm"),
		aliasPath:    filepath.Join(binDirectory, "cterm"),
		manifestPath: manifestPath,
		pathEntry:    binDirectory,
		configPath:   configPath,
		statePath:    statePath,
		tokenPath:    tokenPath,
	}, nil
}

// pathEqual compares cleaned Unix path names case-sensitively.
func pathEqual(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}
