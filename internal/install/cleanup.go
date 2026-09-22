package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseAndRunDeferredCleanup validates hidden helper arguments before entering
// the platform-specific cleanup routine. It exists only for the private
// Windows self-uninstall process and is not a public CLI contract.
func ParseAndRunDeferredCleanup(args []string) error {
	parent, binary, alias, helper, err := parseDeferredCleanupArgs(args)
	if err != nil {
		return err
	}
	resolvedLayout, err := defaultLayout()
	if err != nil {
		return err
	}
	if !samePathName(binary, resolvedLayout.binaryPath) || !samePathName(alias, resolvedLayout.aliasPath) {
		return fmt.Errorf("deferred-uninstall targets do not match the standard installation layout")
	}
	// The helper path is supplied on the command line by another process, so do
	// not let the hidden command schedule deletion of an arbitrary executable.
	helperDirectory := filepath.Dir(helper)
	relative, err := filepath.Rel(os.TempDir(), helperDirectory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || !strings.HasPrefix(filepath.Base(helperDirectory), "channelterm-uninstall-") {
		return fmt.Errorf("deferred-uninstall helper is outside the expected temporary directory")
	}
	return RunDeferredCleanup(parent, binary, alias, helper)
}
