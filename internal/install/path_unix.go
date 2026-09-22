//go:build linux || darwin

package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	pathBlockStart = "# >>> ChannelTerm PATH >>>"
	pathBlockEnd   = "# <<< ChannelTerm PATH <<<"
)

// unixPathEditor owns one visibly delimited block in a supported shell startup
// file when the installation directory is not already effective.
type unixPathEditor struct{}

// newPathEditor constructs the Unix PATH integration used by Manager.
func newPathEditor() pathEditor {
	return unixPathEditor{}
}

func (unixPathEditor) Ensure(entry string, previous *PathRecord) (PathRecord, bool, error) {
	if previous != nil && previous.Managed {
		// An existing ownership record makes a missing block repairable. Without
		// that record, an existing marker is ambiguous and must not be adopted.
		if err := validateUnixPathRecord(*previous); err != nil {
			return PathRecord{}, false, err
		}
		data, err := os.ReadFile(previous.ShellFile)
		if err == nil && strings.Contains(string(data), previous.Block) {
			return *previous, false, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return PathRecord{}, false, err
		}
		created, err := appendPathBlock(previous.ShellFile, previous.Block)
		if err != nil {
			return PathRecord{}, false, err
		}
		repaired := *previous
		repaired.CreatedFile = repaired.CreatedFile || created
		return repaired, true, nil
	}
	if pathContains(os.Getenv("PATH"), entry) {
		return PathRecord{Entry: entry, Scope: "user"}, false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return PathRecord{}, false, err
	}
	shellFile, fish := unixShellFile(home)
	block := unixPathBlock(entry, fish)
	data, readErr := os.ReadFile(shellFile)
	if readErr == nil && strings.Contains(string(data), pathBlockStart) {
		return PathRecord{}, false, fmt.Errorf("ChannelTerm PATH marker already exists in %q without an installation ownership record", shellFile)
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return PathRecord{}, false, readErr
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		// Record the separator newline as part of our exact block so removal can
		// restore a file that originally had no trailing newline byte-for-byte.
		block = "\n" + block
	}
	created, err := appendPathBlock(shellFile, block)
	if err != nil {
		return PathRecord{}, false, err
	}
	return PathRecord{
		Managed:     true,
		Entry:       entry,
		Scope:       "user",
		ShellFile:   shellFile,
		Block:       block,
		CreatedFile: created,
	}, true, nil
}

func (unixPathEditor) Remove(record PathRecord) (bool, error) {
	if !record.Managed {
		return false, nil
	}
	if err := validateUnixPathRecord(record); err != nil {
		return false, err
	}
	data, err := os.ReadFile(record.ShellFile)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	content := string(data)
	// Exact single-block matching is the ownership boundary: edited or
	// duplicated content is left for the user to resolve manually.
	if strings.Count(content, record.Block) != 1 {
		return false, fmt.Errorf("managed PATH block in %q was modified; leaving it unchanged", record.ShellFile)
	}
	updated := strings.Replace(content, record.Block, "", 1)
	if record.CreatedFile && strings.TrimSpace(updated) == "" {
		if err := os.Remove(record.ShellFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		return true, nil
	}
	info, err := os.Stat(record.ShellFile)
	if err != nil {
		return false, err
	}
	if err := writeFileAtomic(record.ShellFile, []byte(updated), info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func unixShellFile(home string) (string, bool) {
	// Login startup files are used because PATH must be available to future
	// terminals, not merely to a child shell of the installer process.
	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case "fish":
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" || !filepath.IsAbs(configHome) {
			configHome = filepath.Join(home, ".config")
		}
		return filepath.Join(configHome, "fish", "conf.d", "channelterm.fish"), true
	case "zsh":
		return filepath.Join(home, ".zprofile"), false
	case "bash":
		bashProfile := filepath.Join(home, ".bash_profile")
		if _, err := os.Stat(bashProfile); err == nil {
			return bashProfile, false
		}
		bashLogin := filepath.Join(home, ".bash_login")
		if _, err := os.Stat(bashLogin); err == nil {
			return bashLogin, false
		}
	}
	return filepath.Join(home, ".profile"), false
}

// unixPathBlock renders the exact content later required for safe removal.
func unixPathBlock(entry string, fish bool) string {
	quoted := shellSingleQuote(entry)
	line := "export PATH=" + quoted + ":\"$PATH\""
	if fish {
		line = "fish_add_path -g " + quoted
	}
	return pathBlockStart + "\n" + line + "\n" + pathBlockEnd + "\n"
}

// appendPathBlock atomically appends a block while preserving existing mode
// bits and reports whether it created the startup file.
func appendPathBlock(path, block string) (bool, error) {
	data, err := os.ReadFile(path)
	created := errors.Is(err, os.ErrNotExist)
	if err != nil && !created {
		return false, err
	}
	data = append(data, []byte(block)...)
	mode := os.FileMode(0o600)
	if !created {
		info, err := os.Stat(path)
		if err != nil {
			return false, err
		}
		mode = info.Mode().Perm()
	}
	if err := writeFileAtomic(path, data, mode); err != nil {
		return false, err
	}
	return created, nil
}

// pathContains compares individual PATH elements using platform path rules.
func pathContains(value, entry string) bool {
	for _, candidate := range filepath.SplitList(value) {
		if candidate != "" && pathEqual(candidate, entry) {
			return true
		}
	}
	return false
}

// shellSingleQuote produces a POSIX/fish-compatible single-quoted literal.
func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// validateUnixPathRecord prevents manifest-controlled edits outside the startup
// files and exact block formats the installer itself can generate.
func validateUnixPathRecord(record PathRecord) error {
	if record.ShellFile == "" || record.Block == "" || record.Entry == "" || record.Scope != "user" {
		return errors.New("managed PATH record is incomplete")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	allowed := map[string]bool{
		filepath.Join(home, ".profile"):      false,
		filepath.Join(home, ".bash_profile"): false,
		filepath.Join(home, ".bash_login"):   false,
		filepath.Join(home, ".zprofile"):     false,
	}
	// A manifest is data, not authority to edit arbitrary files. Restrict it to
	// the small set of startup files the installer itself can choose.
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" || !filepath.IsAbs(configHome) {
		configHome = filepath.Join(home, ".config")
	}
	allowed[filepath.Join(configHome, "fish", "conf.d", "channelterm.fish")] = true
	fish, ok := allowed[filepath.Clean(record.ShellFile)]
	if !ok {
		return fmt.Errorf("managed PATH file %q is outside ChannelTerm's supported shell locations", record.ShellFile)
	}
	expected := unixPathBlock(record.Entry, fish)
	if record.Block != expected && record.Block != "\n"+expected {
		return errors.New("managed PATH block does not match the recorded entry")
	}
	return nil
}
