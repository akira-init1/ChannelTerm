package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateCreatesMinimalTemplateOnlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	file, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	if file.Serial.Default != "" || len(file.Serial.Profiles) != 0 {
		t.Errorf("new file = %+v, want an empty serial configuration", file)
	}
	policy, err := file.ConnectionPolicy()
	if err != nil {
		t.Fatalf("ConnectionPolicy() error = %v", err)
	}
	if policy != "ask" {
		t.Errorf("ConnectionPolicy() = %q, want ask", policy)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(before), "[serial]") {
		t.Errorf("template = %q, want serial section", before)
	}
	if !strings.Contains(string(before), "[connection]") {
		t.Errorf("template = %q, want connection section", before)
	}
	if _, err := LoadOrCreate(path); err != nil {
		t.Fatalf("second LoadOrCreate() error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() after second load error = %v", err)
	}
	if string(after) != string(before) {
		t.Error("LoadOrCreate() changed an existing configuration file")
	}
}

func TestLoadResolvesConnectionPolicyAndRejectsInvalidValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	for _, tt := range []struct {
		name    string
		policy  string
		wantErr string
	}{
		{name: "auto", policy: "auto"},
		{name: "deny", policy: "deny"},
		{name: "invalid", policy: "xxx", wantErr: `connection default policy is invalid: "xxx"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			content := "[connection]\ndefault_policy = \"" + tt.policy + "\"\n"
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			file, err := Load(path)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("Load() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			policy, err := file.ConnectionPolicy()
			if err != nil {
				t.Fatalf("ConnectionPolicy() error = %v", err)
			}
			if string(policy) != tt.policy {
				t.Errorf("ConnectionPolicy() = %q, want %q", policy, tt.policy)
			}
		})
	}
}

func TestLoadResolvesDefaultAndMultipleProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `[serial]
default = "imx6ull"

[serial.profiles.imx6ull]
port = "/dev/ttyUSB0"
baud = 115200
wake = false

[serial.profiles.zynq]
port = "/dev/ttyUSB1"
baud = 57600
data_bits = 7
parity = "even"
stop_bits = "2"
wake = true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	file, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defaultProfile, err := file.ResolveSerial("")
	if err != nil {
		t.Fatalf("ResolveSerial(default) error = %v", err)
	}
	if defaultProfile.Port != "/dev/ttyUSB0" || defaultProfile.BaudRate != 115200 || defaultProfile.DataBits != 8 || defaultProfile.Parity != "none" || defaultProfile.StopBits != "1" || defaultProfile.FlowControl != "none" {
		t.Errorf("default profile = %+v, want imx6ull with 8-N-1 defaults", defaultProfile)
	}
	zynq, err := file.ResolveSerial("zynq")
	if err != nil {
		t.Fatalf("ResolveSerial(zynq) error = %v", err)
	}
	if zynq.Port != "/dev/ttyUSB1" || zynq.BaudRate != 57600 || zynq.DataBits != 7 || zynq.Parity != "even" || zynq.StopBits != "2" || zynq.FlowControl != "none" || !zynq.Wake {
		t.Errorf("zynq profile = %+v, want saved settings", zynq)
	}
}

// TestLoadInitializesDefaultPreferencesWithoutChangingProfiles protects legacy
// config.toml files that have serial profiles but no preferences section.
func TestLoadInitializesDefaultPreferencesWithoutChangingProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `[serial]
default = "board"

[serial.profiles.board]
port = "COM11"
baud = 57600
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	file, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if file.Preferences != DefaultPreferences() {
		t.Errorf("Preferences = %#v, want default %#v", file.Preferences, DefaultPreferences())
	}
	profile, err := file.ResolveSerial("")
	if err != nil {
		t.Fatalf("ResolveSerial() error = %v", err)
	}
	if profile.Port != "COM11" || profile.BaudRate != 57600 {
		t.Errorf("resolved profile = %+v, want saved serial profile", profile)
	}
}

// TestResolveSerialDoesNotDependOnPreferences protects the connection-profile
// boundary from future user-preference fields.
func TestResolveSerialDoesNotDependOnPreferences(t *testing.T) {
	serial := Serial{
		Default: "board",
		Profiles: map[string]SerialProfile{
			"board": {Port: "COM12", BaudRate: 38400},
		},
	}
	withoutPreferences, err := (File{Serial: serial}).ResolveSerial("")
	if err != nil {
		t.Fatalf("ResolveSerial() without preferences error = %v", err)
	}
	withPreferences, err := (File{Serial: serial, Preferences: DefaultPreferences()}).ResolveSerial("")
	if err != nil {
		t.Fatalf("ResolveSerial() with preferences error = %v", err)
	}
	if withPreferences != withoutPreferences {
		t.Errorf("ResolveSerial() with preferences = %+v, want %+v", withPreferences, withoutPreferences)
	}
}

// TestSaveSerialProfileDoesNotChangePreferences keeps profile persistence
// ownership independent from the user-preferences model.
func TestSaveSerialProfileDoesNotChangePreferences(t *testing.T) {
	file := File{Preferences: DefaultPreferences()}
	profile := SerialProfile{Port: "COM14", BaudRate: 115200}
	file.SaveSerialProfile("board", profile)
	if file.Serial.Default != "board" || file.Serial.Profiles["board"] != profile {
		t.Errorf("SaveSerialProfile() file = %+v, want board profile and default", file)
	}
	if file.Preferences != DefaultPreferences() {
		t.Errorf("SaveSerialProfile() preferences = %#v, want default %#v", file.Preferences, DefaultPreferences())
	}
}

func TestResolveSerialRejectsUnknownProfileAndMissingPort(t *testing.T) {
	file := File{Serial: Serial{Profiles: map[string]SerialProfile{"empty": {}}}}
	if _, err := file.ResolveSerial("missing"); !errors.Is(err, ErrProfileNotFound) {
		t.Errorf("ResolveSerial(missing) error = %v, want ErrProfileNotFound", err)
	}
	profile, err := file.ResolveSerial("empty")
	if err != nil {
		t.Fatalf("ResolveSerial(empty) error = %v", err)
	}
	if err := RequireSerialPort(profile); !errors.Is(err, ErrSerialPortRequired) {
		t.Errorf("RequireSerialPort() error = %v, want ErrSerialPortRequired", err)
	}
}

func TestApplySerialOverridesTakesPrecedence(t *testing.T) {
	port := "COM9"
	baud := 9600
	wake := true
	flowControl := "hardware"
	profile := ApplySerialOverrides(SerialProfile{Port: "COM3", BaudRate: 115200, DataBits: 8, Parity: "none", StopBits: "1"}, SerialOverrides{
		Port:        &port,
		BaudRate:    &baud,
		FlowControl: &flowControl,
		Wake:        &wake,
	})
	if profile.Port != "COM9" || profile.BaudRate != 9600 || profile.FlowControl != "hardware" || !profile.Wake {
		t.Errorf("ApplySerialOverrides() = %+v, want CLI overrides", profile)
	}
}

func TestSaveRoundTripsProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	want := File{Serial: Serial{
		Default: "zynq",
		Profiles: map[string]SerialProfile{
			"zynq": {Port: "COM7", BaudRate: 115200, DataBits: 8, Parity: "none", StopBits: "1", FlowControl: "none", Wake: true},
		},
	}}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(data), "[preferences]") {
		t.Errorf("saved configuration = %q, must not add an empty preferences section", data)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	profile, err := got.ResolveSerial("zynq")
	if err != nil {
		t.Fatalf("ResolveSerial() error = %v", err)
	}
	if profile != want.Serial.Profiles["zynq"] {
		t.Errorf("saved profile = %+v, want %+v", profile, want.Serial.Profiles["zynq"])
	}
}

func TestDefaultPathUsesUserConfigDirectory(t *testing.T) {
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir() error = %v", err)
	}
	want := filepath.Join(directory, applicationDirectory, configurationFile)
	if path != want {
		t.Errorf("DefaultPath() = %q, want %q", path, want)
	}
}

func TestDefaultStatePathUsesUserConfigDirectory(t *testing.T) {
	path, err := DefaultStatePath()
	if err != nil {
		t.Fatalf("DefaultStatePath() error = %v", err)
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir() error = %v", err)
	}
	want := filepath.Join(directory, applicationDirectory, stateFile)
	if path != want {
		t.Errorf("DefaultStatePath() = %q, want %q", path, want)
	}
}
