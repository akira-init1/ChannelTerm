package command

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	selfinstall "github.com/akira-init1/ChannelTerm/internal/install"
)

type fakeInstallationManager struct {
	installOptions   selfinstall.InstallOptions
	uninstallOptions selfinstall.UninstallOptions
	installResult    selfinstall.InstallResult
	uninstallResult  selfinstall.UninstallResult
	uninstallCalls   int
	err              error
}

func (manager *fakeInstallationManager) Install(options selfinstall.InstallOptions) (selfinstall.InstallResult, error) {
	manager.installOptions = options
	return manager.installResult, manager.err
}

func (manager *fakeInstallationManager) Uninstall(options selfinstall.UninstallOptions) (selfinstall.UninstallResult, error) {
	manager.uninstallCalls++
	manager.uninstallOptions = options
	return manager.uninstallResult, manager.err
}

func TestRunInstallParsesSafetyOptionsAndReportsPaths(t *testing.T) {
	manager := &fakeInstallationManager{installResult: selfinstall.InstallResult{
		Action: "updated", Binary: "bin/channelterm", Alias: "bin/cterm", Manifest: "state/install.json", Configuration: "config/config.toml", PathChanged: true,
	}}
	var output bytes.Buffer
	if err := runInstallWithManager([]string{"--adopt", "--allow-downgrade"}, &output, manager); err != nil {
		t.Fatal(err)
	}
	if !manager.installOptions.Adopt || !manager.installOptions.AllowDowngrade {
		t.Errorf("install options = %#v", manager.installOptions)
	}
	for _, want := range []string{"updated", "bin/channelterm", "bin/cterm", "state/install.json", "Open a new terminal"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("install output = %q, want %q", output.String(), want)
		}
	}
}

func TestRunUninstallRequiresExplicitPurgeConfirmation(t *testing.T) {
	for _, answer := range []string{"n\n", "N\n", "yes\n", "delete\n", "\n", "other\n"} {
		t.Run("reject_"+strings.TrimSpace(answer), func(t *testing.T) {
			manager := &fakeInstallationManager{}
			var output bytes.Buffer
			err := runUninstallWithManager([]string{"--purge"}, strings.NewReader(answer), &output, manager)
			if err == nil || !strings.Contains(err.Error(), "cancelled") {
				t.Fatalf("confirmation %q error = %v, want cancellation", answer, err)
			}
			if manager.uninstallCalls != 0 {
				t.Fatalf("confirmation %q reached manager", answer)
			}
			if !strings.Contains(output.String(), "Continue? [y/N]:") {
				t.Fatalf("confirmation prompt = %q", output.String())
			}
		})
	}

	for _, answer := range []string{"y\n", "Y\n"} {
		t.Run("accept_"+strings.TrimSpace(answer), func(t *testing.T) {
			manager := &fakeInstallationManager{}
			manager.uninstallResult.Purged = []string{"config.toml", "state.json"}
			var output bytes.Buffer
			if err := runUninstallWithManager([]string{"--purge"}, strings.NewReader(answer), &output, manager); err != nil {
				t.Fatal(err)
			}
			if manager.uninstallCalls != 1 || !manager.uninstallOptions.Purge || !strings.Contains(output.String(), "Purged 2") {
				t.Errorf("confirmed purge = %#v, %q", manager.uninstallOptions, output.String())
			}
		})
	}
}

func TestRunInstallAndUninstallRejectUnexpectedArguments(t *testing.T) {
	manager := &fakeInstallationManager{err: errors.New("must not be called")}
	if err := runInstallWithManager([]string{"extra"}, &bytes.Buffer{}, manager); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Errorf("install argument error = %v", err)
	}
	if err := runUninstallWithManager([]string{"--yes"}, strings.NewReader(""), &bytes.Buffer{}, manager); err == nil || !strings.Contains(err.Error(), "only with --purge") {
		t.Errorf("uninstall --yes error = %v", err)
	}
}

func TestRunRoutesInstallAndUninstallHelp(t *testing.T) {
	for _, command := range []string{"install", "uninstall"} {
		var output bytes.Buffer
		if err := run([]string{command, "--help"}, &output); err != nil {
			t.Fatalf("run(%s --help) error = %v", command, err)
		}
		if !strings.Contains(output.String(), "Usage: channelterm "+command) {
			t.Errorf("run(%s --help) output = %q", command, output.String())
		}
	}
}
