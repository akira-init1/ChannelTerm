package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
)

// ErrSerialProfileExists requires an explicit replacement decision when saving.
var ErrSerialProfileExists = errors.New("serial profile already exists")

// SerialProfiles reports resolved profiles and the persisted default name.
// It contains no configuration path or unrelated configuration sections.
type SerialProfiles struct {
	Default  string
	Profiles []SerialProfileInfo
}

// ReadSerialProfiles reads the Host's configured profile file without creating
// it or opening hardware. The same path resolver is used by OpenSerial.
func (a *Application) ReadSerialProfiles(ctx context.Context) (SerialProfiles, error) {
	if err := ctx.Err(); err != nil {
		return SerialProfiles{}, err
	}
	path, err := a.serial.configPath()
	if err != nil {
		return SerialProfiles{}, err
	}
	file, err := config.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return SerialProfiles{Profiles: []SerialProfileInfo{}}, nil
	}
	if err != nil {
		return SerialProfiles{}, err
	}
	result := SerialProfiles{Default: file.Serial.Default, Profiles: []SerialProfileInfo{}}
	names := make([]string, 0, len(file.Serial.Profiles))
	for name := range file.Serial.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		profile, err := file.ResolveSerial(name)
		if err != nil {
			return SerialProfiles{}, err
		}
		result.Profiles = append(result.Profiles, SerialProfileInfo{Name: name, Profile: profile})
	}
	return result, ctx.Err()
}

// SaveSerialProfile persists a complete profile without connecting. Existing
// names require replace; the first saved profile becomes default as in the CLI.
// SetSerialDefault provides an independent, explicit default-selection operation.
func (a *Application) SaveSerialProfile(ctx context.Context, name string, profile config.SerialProfile, replace bool) error {
	if strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) {
		return errors.New("invalid serial profile name")
	}
	if err := config.RequireSerialPort(profile); err != nil {
		return err
	}
	// Construction validates transport settings but performs no device I/O.
	if _, err := serialtransport.New(serialProfileToTransportConfig(profile)); err != nil {
		return fmt.Errorf("validate serial profile: %w", err)
	}
	path, err := a.serial.configPath()
	if err != nil {
		return err
	}
	return config.UpdateContext(ctx, path, func(file *config.File) error {
		if _, exists := file.Serial.Profiles[name]; exists && !replace {
			return ErrSerialProfileExists
		}
		file.SaveSerialProfile(name, profile)
		return nil
	})
}

// SetSerialDefault selects an existing profile in the same locked configuration
// transaction used by saves. It neither rewrites that profile nor opens a device.
func (a *Application) SetSerialDefault(ctx context.Context, name string) error {
	path, err := a.serial.configPath()
	if err != nil {
		return err
	}
	return config.UpdateContext(ctx, path, func(file *config.File) error {
		if _, exists := file.Serial.Profiles[name]; !exists {
			return config.ErrProfileNotFound
		}
		file.Serial.Default = name
		return nil
	})
}
