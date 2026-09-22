// Package install owns ChannelTerm's per-user executable installation,
// installation manifest, command alias, PATH integration, and uninstall
// lifecycle. It does not own user configuration semantics.
package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
)

const manifestSchemaVersion = 1

// BuildInfo identifies the currently running ChannelTerm binary. Release
// builds populate these values through linker flags; development builds retain
// explicit development provenance.
type BuildInfo struct {
	// Version is the release or development version reported by this binary.
	Version string
	// Commit is the source revision embedded at build time.
	Commit string
	// BuiltAt is the build timestamp embedded by the release process.
	BuiltAt string
}

// InstallOptions controls optional and potentially destructive installation
// choices.
type InstallOptions struct {
	// NoPath skips adding the installation directory to the user PATH.
	NoPath bool
	// Adopt permits matching command files that have no ownership manifest.
	Adopt bool
	// AllowDowngrade permits an older stable release to replace a newer one.
	AllowDowngrade bool
}

// InstallResult describes the completed per-user installation.
type InstallResult struct {
	// Action is installed, updated, repaired, or adopted.
	Action string
	// Source is the executable from which installation was performed.
	Source string
	// Binary is the installed full-command path.
	Binary string
	// Alias is the installed short-command path.
	Alias string
	// Manifest is the ownership-record path.
	Manifest string
	// Configuration is the validated or initialized configuration path.
	Configuration string
	// PathChanged reports whether persistent user PATH state was modified.
	PathChanged bool
}

// UninstallOptions controls removal of user-owned runtime data. Ordinary
// uninstall always preserves configuration, device state, and credentials.
type UninstallOptions struct {
	// Purge also removes the allowlisted default user-data files.
	Purge bool
}

// UninstallResult describes what an uninstall removed. Deferred is true on
// Windows when a helper must delete the running executable after process exit.
type UninstallResult struct {
	// Binary is the removed full-command path.
	Binary string
	// Alias is the removed short-command path.
	Alias string
	// Manifest is the removed ownership-record path.
	Manifest string
	// PathChanged reports whether persistent user PATH state was modified.
	PathChanged bool
	// Purged lists user-data files removed by an explicit purge.
	Purged []string
	// Deferred reports that Windows scheduled command removal after process exit.
	Deferred bool
}

// Manifest is the versioned ownership record written by the built-in
// installer. Paths read from it are never trusted until they match the current
// platform's expected layout.
type Manifest struct {
	// SchemaVersion identifies the manifest format understood by the installer.
	SchemaVersion int `json:"schema_version"`
	// Version, Commit, and BuiltAt preserve installed build provenance.
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
	// InstalledAt is retained across updates; UpdatedAt changes on every install.
	InstalledAt time.Time `json:"installed_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// BinaryPath and BinarySHA256 identify the installer-owned executable.
	BinaryPath   string `json:"binary_path"`
	BinarySHA256 string `json:"binary_sha256"`
	// AliasPath and AliasKind describe the platform's short command.
	AliasPath string `json:"alias_path"`
	AliasKind string `json:"alias_kind"`
	// Path records the persistent environment change, if ChannelTerm made one.
	Path PathRecord `json:"path"`
}

// PathRecord records only PATH changes made by ChannelTerm. Uninstall uses the
// exact record to avoid removing a path that predates the installation.
type PathRecord struct {
	// Managed distinguishes installer-owned entries from preexisting PATH state.
	Managed bool `json:"managed"`
	// Entry is the normalized command directory added to PATH.
	Entry string `json:"entry,omitempty"`
	// Scope is "user" for all supported production implementations.
	Scope string `json:"scope,omitempty"`
	// ShellFile and Block identify the exact Unix startup-file edit.
	ShellFile string `json:"shell_file,omitempty"`
	Block     string `json:"block,omitempty"`
	// CreatedFile reports that the Unix startup file did not previously exist.
	CreatedFile bool `json:"created_file,omitempty"`
}

// layout contains the only paths the installer is permitted to own on the
// current platform.
type layout struct {
	binaryPath   string
	aliasPath    string
	manifestPath string
	pathEntry    string
	configPath   string
	statePath    string
	tokenPath    string
}

// pathEditor isolates the platform-specific representation of the current
// user's PATH. Implementations must remove only entries described as managed
// by the supplied ownership record.
type pathEditor interface {
	Ensure(string, *PathRecord) (PathRecord, bool, error)
	Remove(PathRecord) (bool, error)
}

// Manager performs one platform-scoped install or uninstall transaction.
// Construct it with NewDefault for production use.
type Manager struct {
	build      BuildInfo
	layout     layout
	paths      pathEditor
	sourcePath string
	now        func() time.Time
}

// NewDefault resolves the current executable and the current user's standard
// installation locations without changing the filesystem.
func NewDefault(build BuildInfo) (*Manager, error) {
	sourcePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve current executable: %w", err)
	}
	sourcePath, err = filepath.Abs(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute executable path: %w", err)
	}
	resolvedLayout, err := defaultLayout()
	if err != nil {
		return nil, err
	}
	return &Manager{
		build:      build,
		layout:     resolvedLayout,
		paths:      newPathEditor(),
		sourcePath: filepath.Clean(sourcePath),
		now:        time.Now,
	}, nil
}

// Install copies the running binary into the standard per-user location,
// creates its short command, optionally manages PATH, and initializes the
// existing ChannelTerm configuration lifecycle.
func (m *Manager) Install(options InstallOptions) (_ InstallResult, returnErr error) {
	if err := m.validate(); err != nil {
		return InstallResult{}, err
	}
	sourceInfo, err := os.Stat(m.sourcePath)
	if err != nil {
		return InstallResult{}, fmt.Errorf("inspect source executable %q: %w", m.sourcePath, err)
	}
	if !sourceInfo.Mode().IsRegular() {
		return InstallResult{}, fmt.Errorf("source executable %q is not a regular file", m.sourcePath)
	}
	// Hash before inspecting targets so --adopt can prove that an unmanaged
	// regular file is exactly the executable the user asked us to install.
	sourceHash, err := hashFile(m.sourcePath)
	if err != nil {
		return InstallResult{}, fmt.Errorf("hash source executable: %w", err)
	}

	previous, manifestExists, err := loadManifest(m.layout.manifestPath)
	if err != nil {
		return InstallResult{}, err
	}
	if manifestExists {
		if err := m.validateManifest(previous); err != nil {
			return InstallResult{}, err
		}
		if err := verifyOwnedBinary(previous); err != nil {
			return InstallResult{}, err
		}
		if _, err := os.Lstat(previous.AliasPath); err == nil {
			if err := verifyAlias(previous.BinaryPath, previous.AliasPath, previous.AliasKind); err != nil {
				return InstallResult{}, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return InstallResult{}, fmt.Errorf("inspect short command %q: %w", previous.AliasPath, err)
		}
		if compareVersions(m.build.Version, previous.Version) < 0 && !options.AllowDowngrade {
			return InstallResult{}, fmt.Errorf("installed version %s is newer than source version %s; use --allow-downgrade to replace it", previous.Version, m.build.Version)
		}
	} else if err := m.preflightUnmanagedTargets(sourceHash, options.Adopt); err != nil {
		return InstallResult{}, err
	}
	if err := validateExistingConfiguration(m.layout.configPath); err != nil {
		return InstallResult{}, err
	}
	if _, err := config.LoadOrCreate(m.layout.configPath); err != nil {
		return InstallResult{}, fmt.Errorf("initialize configuration: %w", err)
	}

	// Program files and the manifest form one logical transaction. Snapshots
	// let a later alias, PATH, or manifest failure restore the user's previous
	// installation instead of leaving a mixed-version command pair behind.
	binarySnapshot, err := snapshotFile(m.layout.binaryPath)
	if err != nil {
		return InstallResult{}, fmt.Errorf("snapshot installed executable: %w", err)
	}
	aliasSnapshot, err := snapshotFile(m.layout.aliasPath)
	if err != nil {
		binarySnapshot.discard()
		return InstallResult{}, fmt.Errorf("snapshot short command: %w", err)
	}
	manifestSnapshot, err := snapshotFile(m.layout.manifestPath)
	if err != nil {
		binarySnapshot.discard()
		aliasSnapshot.discard()
		return InstallResult{}, fmt.Errorf("snapshot installation manifest: %w", err)
	}
	committed := false
	pathChanged := false
	binaryChanged := false
	aliasChanged := false
	manifestChanged := false
	pathRecord := PathRecord{Entry: m.layout.pathEntry}
	defer func() {
		defer binarySnapshot.discard()
		defer aliasSnapshot.discard()
		defer manifestSnapshot.discard()
		if committed {
			return
		}
		// Roll back in reverse mutation order. Errors are joined with the
		// original failure because losing rollback diagnostics would hide a
		// potentially recoverable partial installation.
		var rollbackErrors []error
		if manifestChanged {
			if err := manifestSnapshot.restore(); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore installation manifest: %w", err))
			}
		}
		if pathChanged {
			if _, err := m.paths.Remove(pathRecord); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("roll back PATH: %w", err))
			}
		}
		if aliasChanged {
			if err := aliasSnapshot.restore(); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore short command: %w", err))
			}
		}
		if binaryChanged {
			if err := binarySnapshot.restore(); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore installed executable: %w", err))
			}
		}
		if len(rollbackErrors) > 0 {
			returnErr = errors.Join(append([]error{returnErr}, rollbackErrors...)...)
		}
	}()

	action := "installed"
	if manifestExists {
		action = "updated"
		if previous.BinarySHA256 == sourceHash {
			action = "repaired"
		}
	} else if options.Adopt && (sameFilePath(m.sourcePath, m.layout.binaryPath) || sameFilePath(m.sourcePath, m.layout.aliasPath)) {
		action = "adopted"
	}

	if !sameFilePath(m.sourcePath, m.layout.binaryPath) || sourceHash != fileHashOrEmpty(m.layout.binaryPath) {
		binaryChanged = true
		if err := copyExecutableAtomic(m.sourcePath, m.layout.binaryPath); err != nil {
			return InstallResult{}, err
		}
	}
	// Verify the installed bytes independently of the atomic replacement. This
	// checksum is also the proof of ownership used by a future update/removal.
	installedHash, err := hashFile(m.layout.binaryPath)
	if err != nil {
		return InstallResult{}, fmt.Errorf("verify installed executable: %w", err)
	}
	if installedHash != sourceHash {
		return InstallResult{}, fmt.Errorf("installed executable checksum %s does not match source %s", installedHash, sourceHash)
	}
	aliasKind, installedAlias, err := installAlias(m.layout.binaryPath, m.layout.aliasPath)
	aliasChanged = installedAlias
	if err != nil {
		return InstallResult{}, err
	}

	if manifestExists {
		pathRecord = previous.Path
	}
	if !options.NoPath {
		var prior *PathRecord
		if manifestExists {
			prior = &previous.Path
		}
		pathRecord, pathChanged, err = m.paths.Ensure(m.layout.pathEntry, prior)
		if err != nil {
			return InstallResult{}, fmt.Errorf("configure user PATH: %w", err)
		}
	}
	now := m.now().UTC()
	installedAt := now
	if manifestExists {
		installedAt = previous.InstalledAt
	}
	manifest := Manifest{
		SchemaVersion: manifestSchemaVersion,
		Version:       m.build.Version,
		Commit:        m.build.Commit,
		BuiltAt:       m.build.BuiltAt,
		InstalledAt:   installedAt,
		UpdatedAt:     now,
		BinaryPath:    m.layout.binaryPath,
		BinarySHA256:  installedHash,
		AliasPath:     m.layout.aliasPath,
		AliasKind:     aliasKind,
		Path:          pathRecord,
	}
	// Write the manifest last: its presence is the commit record that permits a
	// later invocation to update or remove the installed files.
	manifestChanged = true
	if err := writeManifest(m.layout.manifestPath, manifest); err != nil {
		return InstallResult{}, err
	}
	committed = true
	return InstallResult{
		Action:        action,
		Source:        m.sourcePath,
		Binary:        m.layout.binaryPath,
		Alias:         m.layout.aliasPath,
		Manifest:      m.layout.manifestPath,
		Configuration: m.layout.configPath,
		PathChanged:   pathChanged,
	}, nil
}

// Uninstall removes only files and PATH changes proven to be owned by the
// installation manifest. Purge additionally removes the known ChannelTerm
// configuration, device state, and local HTTP credential files.
func (m *Manager) Uninstall(options UninstallOptions) (UninstallResult, error) {
	if err := m.validate(); err != nil {
		return UninstallResult{}, err
	}
	manifest, exists, err := loadManifest(m.layout.manifestPath)
	if err != nil {
		return UninstallResult{}, err
	}
	if !exists {
		return UninstallResult{}, fmt.Errorf("ChannelTerm installation manifest %q does not exist", m.layout.manifestPath)
	}
	if err := m.validateManifest(manifest); err != nil {
		return UninstallResult{}, err
	}
	// Treat the manifest as untrusted input. Matching standard paths and
	// checksums prevents a modified manifest from turning uninstall into an
	// arbitrary-file deletion primitive.
	if err := verifyOwnedBinary(manifest); err != nil {
		return UninstallResult{}, err
	}
	if err := verifyAlias(manifest.BinaryPath, manifest.AliasPath, manifest.AliasKind); err != nil {
		return UninstallResult{}, err
	}

	pathChanged, err := m.paths.Remove(manifest.Path)
	if err != nil {
		return UninstallResult{}, fmt.Errorf("remove managed PATH entry: %w", err)
	}
	deferred, err := removeInstalledFiles(manifest.BinaryPath, manifest.AliasPath)
	if err != nil {
		// PATH was the only mutation so far. Best-effort restoration keeps the
		// still-installed command discoverable if file removal fails.
		if pathChanged {
			_, _, _ = m.paths.Ensure(manifest.Path.Entry, &manifest.Path)
		}
		return UninstallResult{}, err
	}

	purged := []string{}
	if options.Purge {
		// Purge is intentionally an allowlist. Files added by users or future
		// versions are never inferred from the containing directory.
		for _, path := range []string{
			m.layout.configPath,
			m.layout.configPath + ".lock",
			m.layout.statePath,
			m.layout.tokenPath,
			m.layout.tokenPath + ".lock",
		} {
			if err := os.Remove(path); err == nil {
				purged = append(purged, path)
			} else if !errors.Is(err, os.ErrNotExist) {
				return UninstallResult{}, fmt.Errorf("purge %q: %w", path, err)
			}
		}
	}
	if err := os.Remove(m.layout.manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return UninstallResult{}, fmt.Errorf("remove installation manifest: %w", err)
	}
	removeEmptyParents(m.layout.manifestPath, filepath.Dir(m.layout.manifestPath))
	if options.Purge {
		removeEmptyParents(m.layout.configPath, filepath.Dir(m.layout.configPath))
	}
	return UninstallResult{
		Binary:      manifest.BinaryPath,
		Alias:       manifest.AliasPath,
		Manifest:    m.layout.manifestPath,
		PathChanged: pathChanged,
		Purged:      purged,
		Deferred:    deferred,
	}, nil
}

func (m *Manager) validate() error {
	if m.paths == nil || m.sourcePath == "" || m.layout.binaryPath == "" || m.layout.aliasPath == "" || m.layout.manifestPath == "" {
		return errors.New("installer is missing required dependencies")
	}
	if samePathName(m.layout.binaryPath, m.layout.aliasPath) {
		return errors.New("installed command and short command paths must differ")
	}
	return nil
}

// validateManifest confines every recorded removal target to the layout
// independently resolved for the current user and platform.
func (m *Manager) validateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != manifestSchemaVersion {
		return fmt.Errorf("unsupported installation manifest schema %d", manifest.SchemaVersion)
	}
	if !samePathName(manifest.BinaryPath, m.layout.binaryPath) || !samePathName(manifest.AliasPath, m.layout.aliasPath) {
		return fmt.Errorf("installation manifest paths do not match the standard installation layout")
	}
	if manifest.Path.Entry != "" && !samePathName(manifest.Path.Entry, m.layout.pathEntry) {
		return fmt.Errorf("installation manifest PATH entry %q is outside the standard installation layout", manifest.Path.Entry)
	}
	return nil
}

// preflightUnmanagedTargets refuses to overwrite paths for which ChannelTerm
// has no ownership record. Adoption is deliberately limited to byte-identical
// files or the expected alias symlink.
func (m *Manager) preflightUnmanagedTargets(sourceHash string, adopt bool) error {
	for _, target := range []string{m.layout.binaryPath, m.layout.aliasPath} {
		info, err := os.Lstat(target)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect existing installation target %q: %w", target, err)
		}
		if info.IsDir() {
			return fmt.Errorf("installation target %q is a directory; move or rename it before installing", target)
		}
		if !adopt {
			return fmt.Errorf("installation target %q exists without a ChannelTerm ownership record; use --adopt only if you intend to replace it", target)
		}
		if info.Mode().IsRegular() {
			hash, hashErr := hashFile(target)
			if hashErr != nil {
				return hashErr
			}
			if hash != sourceHash {
				return fmt.Errorf("cannot adopt %q because its checksum differs from the running ChannelTerm binary", target)
			}
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, resolveErr := filepath.EvalSymlinks(target)
			if resolveErr != nil || !sameFilePath(resolved, m.layout.binaryPath) {
				return fmt.Errorf("cannot adopt short command %q because it does not resolve to %q", target, m.layout.binaryPath)
			}
			continue
		}
		return fmt.Errorf("installation target %q has unsupported file type %s", target, info.Mode())
	}
	return nil
}

// validateExistingConfiguration distinguishes an absent configuration from a
// malformed one. The latter must stop installation rather than being silently
// replaced by LoadOrCreate.
func validateExistingConfiguration(path string) error {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect configuration %q: %w", path, err)
	}
	if _, err := config.Load(path); err != nil {
		return fmt.Errorf("validate existing configuration: %w", err)
	}
	return nil
}

// loadManifest decodes strictly so a manifest written by an incompatible
// implementation cannot silently lose ownership information.
func loadManifest(path string) (Manifest, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, false, nil
	}
	if err != nil {
		return Manifest{}, false, fmt.Errorf("read installation manifest %q: %w", path, err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, false, fmt.Errorf("decode installation manifest %q: %w", path, err)
	}
	return manifest, true, nil
}

// writeManifest persists the ownership record atomically with user-only
// permissions because it contains authoritative removal paths.
func writeManifest(path string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode installation manifest: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write installation manifest %q: %w", path, err)
	}
	return nil
}

// verifyOwnedBinary requires the installed bytes to still match the last
// committed installation before update or removal is allowed.
func verifyOwnedBinary(manifest Manifest) error {
	hash, err := hashFile(manifest.BinaryPath)
	if err != nil {
		return fmt.Errorf("verify installed executable %q: %w", manifest.BinaryPath, err)
	}
	if hash != manifest.BinarySHA256 {
		return fmt.Errorf("installed executable %q was modified outside ChannelTerm; expected SHA-256 %s, got %s", manifest.BinaryPath, manifest.BinarySHA256, hash)
	}
	return nil
}

// hashFile returns a lowercase SHA-256 digest for ownership comparisons.
func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// fileHashOrEmpty supports the install fast path; any read failure forces an
// attempted replacement, where the caller can report the concrete error.
func fileHashOrEmpty(path string) string {
	hash, err := hashFile(path)
	if err != nil {
		return ""
	}
	return hash
}

// copyExecutableAtomic installs a regular executable with user-executable
// permission bits on Unix; Windows ignores those mode bits.
func copyExecutableAtomic(source, destination string) error {
	return copyFileAtomic(source, destination, 0o755)
}

// copyFileAtomic fsyncs the temporary file before an atomic same-directory
// replacement, then syncs the parent where the platform supports it.
func copyFileAtomic(source, destination string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create executable directory: %w", err)
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open source executable: %w", err)
	}
	defer input.Close()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".channelterm-install-*")
	if err != nil {
		return fmt.Errorf("create temporary executable: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set executable permissions: %w", err)
	}
	if _, err := io.Copy(temporary, input); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("copy executable: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync executable: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close executable: %w", err)
	}
	if err := replaceFile(temporaryPath, destination); err != nil {
		return fmt.Errorf("install executable %q: %w", destination, err)
	}
	return syncParentDirectory(filepath.Dir(destination))
}

// fileSnapshot retains just enough information to undo replacement of an
// absent file, regular file, or symbolic link during an install transaction.
type fileSnapshot struct {
	path       string
	exists     bool
	mode       os.FileMode
	linkTarget string
	backupPath string
}

// snapshotFile copies regular-file contents to the system temporary directory
// so the destination can be restored even after an atomic replacement.
func snapshotFile(path string) (fileSnapshot, error) {
	snapshot := fileSnapshot{path: path}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	if info.IsDir() {
		return snapshot, fmt.Errorf("%q is a directory", path)
	}
	snapshot.exists = true
	snapshot.mode = info.Mode()
	if info.Mode()&os.ModeSymlink != 0 {
		snapshot.linkTarget, err = os.Readlink(path)
		return snapshot, err
	}
	if !info.Mode().IsRegular() {
		return snapshot, fmt.Errorf("%q has unsupported file type %s", path, info.Mode())
	}
	backup, err := os.CreateTemp("", "channelterm-install-backup-*")
	if err != nil {
		return snapshot, err
	}
	snapshot.backupPath = backup.Name()
	input, err := os.Open(path)
	if err != nil {
		_ = backup.Close()
		snapshot.discard()
		return fileSnapshot{}, err
	}
	_, copyErr := io.Copy(backup, input)
	closeInputErr := input.Close()
	closeBackupErr := backup.Close()
	if err := errors.Join(copyErr, closeInputErr, closeBackupErr); err != nil {
		snapshot.discard()
		return fileSnapshot{}, err
	}
	return snapshot, nil
}

// restore returns the snapshotted path to its exact pre-transaction presence
// and file kind. Regular-file permission bits are preserved.
func (snapshot fileSnapshot) restore() error {
	if !snapshot.exists {
		if err := os.Remove(snapshot.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if snapshot.mode&os.ModeSymlink != 0 {
		temporary := snapshot.path + ".channelterm-restore"
		_ = os.Remove(temporary)
		if err := os.Symlink(snapshot.linkTarget, temporary); err != nil {
			return err
		}
		defer os.Remove(temporary)
		return replaceFile(temporary, snapshot.path)
	}
	return copyFileAtomic(snapshot.backupPath, snapshot.path, snapshot.mode.Perm())
}

// discard removes any temporary backup retained by a snapshot.
func (snapshot fileSnapshot) discard() {
	if snapshot.backupPath != "" {
		_ = os.Remove(snapshot.backupPath)
	}
}

// writeFileAtomic durably replaces small installer-owned state files without
// exposing partially written contents to another process.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".channelterm-state-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return err
	}
	return syncParentDirectory(filepath.Dir(path))
}

// sameFilePath answers whether two names currently identify the same file,
// falling back to normalized path comparison when either name does not exist.
func sameFilePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	leftInfo, leftStatErr := os.Stat(leftAbs)
	rightInfo, rightStatErr := os.Stat(rightAbs)
	if leftStatErr == nil && rightStatErr == nil {
		return os.SameFile(leftInfo, rightInfo)
	}
	return pathEqual(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

// samePathName compares normalized path names without following an existing
// symlink. Manifest validation needs names, not inode identity.
func samePathName(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return pathEqual(filepath.Clean(left), filepath.Clean(right))
	}
	return pathEqual(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

// removeEmptyParents removes empty directories from the path upward through
// stop, but never removes non-empty directories or walks outside stop.
func removeEmptyParents(path, stop string) {
	directory := filepath.Dir(path)
	stop = filepath.Clean(stop)
	for pathWithin(directory, stop) {
		if err := os.Remove(directory); err != nil {
			return
		}
		if pathEqual(directory, stop) {
			return
		}
		directory = filepath.Dir(directory)
	}
}

// pathWithin reports whether path is root itself or one of its descendants.
func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// parseReleaseVersion accepts only stable three-component versions, with an
// optional leading v, for conservative downgrade protection.
func parseReleaseVersion(version string) ([]int, bool) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" || strings.ContainsAny(version, "-+") {
		return nil, false
	}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return nil, false
	}
	numbers := make([]int, 3)
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return nil, false
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return nil, false
		}
		numbers[index] = value
	}
	return numbers, true
}

// compareVersions orders only complete stable semantic versions. Development,
// prerelease, build-metadata, and custom version strings are intentionally
// unordered so a local build is not accidentally classified as a downgrade.
func compareVersions(source, installed string) int {
	sourceParts, sourceOK := parseReleaseVersion(source)
	installedParts, installedOK := parseReleaseVersion(installed)
	if !sourceOK || !installedOK {
		return 0
	}
	for index := range sourceParts {
		if sourceParts[index] < installedParts[index] {
			return -1
		}
		if sourceParts[index] > installedParts[index] {
			return 1
		}
	}
	return 0
}
