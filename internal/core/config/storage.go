package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
	"github.com/pelletier/go-toml/v2"
)

const (
	applicationDirectory = "channelterm"
	configurationFile    = "config.toml"
	stateFile            = "state.json"
)

// DefaultPath returns the platform's normal location for config.toml.
//
// The path is based on os.UserConfigDir, which uses the appropriate Windows or
// Unix user configuration directory. It does not create files or directories.
func DefaultPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(directory, applicationDirectory, configurationFile), nil
}

// DefaultStatePath returns the platform's normal location for state.json.
//
// State is kept beside the user configuration but has a separate lifecycle:
// it is maintained by ChannelTerm to retain discovered device identities and
// must never be treated as user connection configuration.
func DefaultStatePath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(directory, applicationDirectory, stateFile), nil
}

// Load reads and decodes a TOML configuration file at path.
//
// It begins with DefaultPreferences so fields added in future releases can
// default correctly when absent from an existing config.toml. It validates the
// connection discovery policy but leaves serial value validation to the serial
// opening boundary, preserving the existing resolution lifecycle.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read configuration %q: %w", path, err)
	}
	file := File{Preferences: DefaultPreferences()}
	if err := toml.Unmarshal(data, &file); err != nil {
		return File{}, fmt.Errorf("decode configuration %q: %w", path, err)
	}
	if _, err := file.ConnectionPolicy(); err != nil {
		return File{}, err
	}
	return file, nil
}

// LoadOrCreate loads a TOML configuration file, creating the minimal template
// when it does not exist. Existing configuration files are read only.
//
// The template intentionally contains no preferences section because the
// current Preferences model has no persisted fields.
func LoadOrCreate(path string) (File, error) {
	file, err := Load(path)
	if err == nil {
		return file, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return File{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return File{}, fmt.Errorf("create configuration directory for %q: %w", path, err)
	}
	configuration, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return Load(path)
	}
	if err != nil {
		return File{}, fmt.Errorf("create configuration %q: %w", path, err)
	}
	if _, err := configuration.WriteString(defaultTemplate); err != nil {
		_ = configuration.Close()
		return File{}, fmt.Errorf("write configuration %q: %w", path, err)
	}
	if err := configuration.Sync(); err != nil {
		_ = configuration.Close()
		return File{}, fmt.Errorf("sync configuration %q: %w", path, err)
	}
	if err := configuration.Close(); err != nil {
		return File{}, fmt.Errorf("close configuration %q: %w", path, err)
	}
	return Load(path)
}

// Save writes file as TOML to path, creating its parent directory if needed.
//
// It persists all populated File sections through their shared config.toml
// lifecycle. An empty Preferences value is omitted, so saving a serial profile
// does not add an empty preferences table.
func Save(path string, file File) error {
	data, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode configuration %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create configuration directory for %q: %w", path, err)
	}
	if err := writeFileAtomically(path, data, 0o600); err != nil {
		return fmt.Errorf("write configuration %q: %w", path, err)
	}
	return nil
}

// Update serializes one read-modify-write transaction across ChannelTerm
// processes. update receives the latest on-disk value while an adjacent lock
// file is held; returning an error leaves the configuration unchanged.
func Update(path string, update func(*File) error) error {
	if update == nil {
		return errors.New("configuration update function must not be nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create configuration directory for %q: %w", path, err)
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("lock configuration %q: %w", path, err)
	}
	defer func() { _ = lock.Unlock() }()
	file, err := LoadOrCreate(path)
	if err != nil {
		return err
	}
	if err := update(&file); err != nil {
		return err
	}
	return Save(path, file)
}

// writeFileAtomically replaces path only after a complete same-directory
// temporary file has reached stable storage. The rename is atomic where the
// operating system provides that guarantee; every platform avoids writing a
// partial new value directly over the previous file.
func writeFileAtomically(path string, data []byte, mode os.FileMode) (err error) {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncDirectory(directory)
}

const defaultTemplate = "# ChannelTerm configuration.\n# Add named serial profiles before connecting without --port.\n\n[serial]\n# default = \"profile-name\"\n\n[connection]\n# default_policy = \"ask\"\n"
