package device

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStateStorePersistsUSBSerialIdentityAcrossEndpointAndPathChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStateStore(path, "dev_a")
	first := mustResolve(t, store, Endpoint{Transport: "serial", Endpoint: "COM6", Metadata: SerialMetadata{VID: "0403", PID: "6010", USBSerial: "ABC123", USBPath: "A"}})
	if got := first["serial\x00COM6"]; got.DeviceID != "dev_a" || got.Method != IdentityUSBSerial || !got.Persistent {
		t.Fatalf("first identity = %#v, want persistent USB serial identity", got)
	}

	reloaded, err := LoadStateStore(path)
	if err != nil {
		t.Fatalf("LoadStateStore() error = %v", err)
	}
	second := mustResolve(t, reloaded, Endpoint{Transport: "serial", Endpoint: "COM11", Metadata: SerialMetadata{VID: "0403", PID: "6010", USBSerial: "ABC123", USBPath: "B"}})
	if got := second["serial\x00COM11"]; got.DeviceID != "dev_a" || got.Method != IdentityUSBSerial || !got.Persistent {
		t.Errorf("moved identity = %#v, want preserved dev_a USB serial identity", got)
	}
	if got := reloaded.state.Devices[0]; got.LastEndpoint != "COM11" || got.Identity.USBPath != "B" {
		t.Errorf("stored device = %#v, want refreshed endpoint and USB path snapshot", got)
	}
}

func TestStateStoreCreatesVersionOneFileAndPersistsUSBPathIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := LoadStateStore(path)
	if err != nil {
		t.Fatalf("LoadStateStore(missing) error = %v", err)
	}
	if store.state.Version != stateVersion || len(store.state.Devices) != 0 {
		t.Fatalf("initial state = %#v, want empty v1 state", store.state)
	}
	first := mustResolve(t, store, Endpoint{Transport: "serial", Endpoint: "COM6", Metadata: SerialMetadata{VID: "1a86", PID: "7523", USBPath: "A"}})

	reloaded, err := LoadStateStore(path)
	if err != nil {
		t.Fatalf("LoadStateStore(reload) error = %v", err)
	}
	second := mustResolve(t, reloaded, Endpoint{Transport: "serial", Endpoint: "COM11", Metadata: SerialMetadata{VID: "1a86", PID: "7523", USBPath: "A"}})
	if first["serial\x00COM6"].DeviceID != second["serial\x00COM11"].DeviceID || second["serial\x00COM11"].Method != IdentityUSBPath {
		t.Errorf("USB path identities = %#v then %#v, want one persisted location identity", first, second)
	}
}

func TestStateStoreUsesDistinctUSBPathIdentitiesForMatchingCH340Adapters(t *testing.T) {
	store := newTestStateStore(filepath.Join(t.TempDir(), "state.json"), "dev_a", "dev_b")
	resolved := mustResolve(t, store,
		Endpoint{Transport: "serial", Endpoint: "COM6", Metadata: SerialMetadata{VID: "1a86", PID: "7523", USBPath: "A"}},
		Endpoint{Transport: "serial", Endpoint: "COM8", Metadata: SerialMetadata{VID: "1a86", PID: "7523", USBPath: "B"}},
	)
	a := resolved["serial\x00COM6"]
	b := resolved["serial\x00COM8"]
	if a.DeviceID == b.DeviceID || a.Method != IdentityUSBPath || b.Method != IdentityUSBPath || !a.Persistent || !b.Persistent {
		t.Errorf("CH340 identities = %#v and %#v, want distinct persistent USB-path identities", a, b)
	}
}

func TestStateStoreDoesNotMatchSeriallessDeviceAfterUSBPathChange(t *testing.T) {
	store := newTestStateStore(filepath.Join(t.TempDir(), "state.json"), "dev_a", "dev_b")
	first := mustResolve(t, store, Endpoint{Transport: "serial", Endpoint: "COM6", Metadata: SerialMetadata{VID: "1a86", PID: "7523", USBPath: "A"}})
	second := mustResolve(t, store, Endpoint{Transport: "serial", Endpoint: "COM11", Metadata: SerialMetadata{VID: "1a86", PID: "7523", USBPath: "C"}})
	if first["serial\x00COM6"].DeviceID == second["serial\x00COM11"].DeviceID {
		t.Error("serial-less device inherited its ID after a USB path change")
	}
}

func TestStateStoreReturnsEphemeralRuntimeIdentityWithoutPersistentEvidence(t *testing.T) {
	store := newTestStateStore(filepath.Join(t.TempDir(), "state.json"), "dev_runtime")
	resolved := mustResolve(t, store, Endpoint{Transport: "serial", Endpoint: "COM9"})
	identity := resolved["serial\x00COM9"]
	if identity.Method != IdentityRuntime || identity.Persistent || identity.DeviceID != "dev_runtime" {
		t.Errorf("runtime identity = %#v, want ephemeral dev_runtime", identity)
	}
	if len(store.state.Devices) != 0 {
		t.Errorf("persistent records = %#v, want no record for endpoint-only device", store.state.Devices)
	}
}

func TestStateStoreWritesOnlyWhenPersistentStateChanges(t *testing.T) {
	writes := 0
	store := newStateStore(filepath.Join(t.TempDir(), "state.json"), time.Now, sequenceIDs("dev_a"), func(string, []byte) error {
		writes++
		return nil
	})
	endpoint := Endpoint{Transport: "serial", Endpoint: "COM6", Metadata: SerialMetadata{VID: "0403", PID: "6010", USBSerial: "ABC123", USBPath: "A"}}
	mustResolve(t, store, endpoint)
	mustResolve(t, store, endpoint)
	if writes != 1 {
		t.Errorf("writes = %d, want one write for unchanged periodic scans", writes)
	}
}

func TestStateStoreRejectsCorruptAndUnsupportedFilesWithoutReplacingThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	corrupt := []byte("{not json")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStateStore(path); err == nil || !strings.Contains(err.Error(), "decode device state") {
		t.Errorf("LoadStateStore(corrupt) error = %v, want decode error", err)
	}
	assertFileBytes(t, path, corrupt)

	unsupported := []byte(`{"version":999,"devices":[]}`)
	if err := os.WriteFile(path, unsupported, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStateStore(path); !errors.Is(err, ErrUnsupportedStateVersion) {
		t.Errorf("LoadStateStore(unsupported) error = %v, want ErrUnsupportedStateVersion", err)
	}
	assertFileBytes(t, path, unsupported)
}

func TestStateStoreFailedSavePreservesOldFileAndRetriesLater(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	old := []byte(`{"version":1,"devices":[]}`)
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	store := newStateStore(path, time.Now, sequenceIDs("dev_a"), func(string, []byte) error { return errors.New("replace failed") })
	if err := store.load(); err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if _, err := store.Resolve([]Endpoint{{Transport: "serial", Endpoint: "COM6", Metadata: SerialMetadata{VID: "0403", PID: "6010", USBSerial: "ABC123"}}}, time.Now()); err == nil {
		t.Error("Resolve() error = nil, want failed replacement")
	}
	assertFileBytes(t, path, old)
	if len(store.state.Devices) != 0 {
		t.Errorf("in-memory devices = %#v, want rollback after failed save", store.state.Devices)
	}
}

func TestRegistryWithStateStoreExposesPersistentIdentity(t *testing.T) {
	store := newTestStateStore(filepath.Join(t.TempDir(), "state.json"), "dev_a")
	registry, err := NewRegistryWithStateStore(ScannerFunc(func(context.Context) ([]Endpoint, error) {
		return []Endpoint{{Transport: "serial", Endpoint: "COM6", Metadata: SerialMetadata{VID: "1a86", PID: "7523", USBPath: "A"}}}, nil
	}), store)
	if err != nil {
		t.Fatalf("NewRegistryWithStateStore() error = %v", err)
	}
	if err := registry.scan(context.Background()); err != nil {
		t.Fatalf("scan() error = %v", err)
	}
	got := registry.List()[0]
	if got.DeviceID != "dev_a" || got.IdentityMethod != IdentityUSBPath || !got.Persistent {
		t.Errorf("Registry Device = %#v, want persisted USB path identity", got)
	}
}

// newTestStateStore uses a real atomic writer while giving assertions stable IDs.
func newTestStateStore(path string, ids ...string) *StateStore {
	return newStateStore(path, time.Now, sequenceIDs(ids...), atomicWrite)
}

// sequenceIDs returns deterministic test IDs and fails loudly when a test needs
// more unique identities than it declared.
func sequenceIDs(ids ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		if index >= len(ids) {
			return "", errors.New("test ID sequence exhausted")
		}
		id := ids[index]
		index++
		return id, nil
	}
}

func mustResolve(t *testing.T, store *StateStore, endpoints ...Endpoint) map[string]Identity {
	t.Helper()
	resolved, err := store.Resolve(endpoints, time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	return resolved
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("file %q = %q, want %q", path, got, want)
	}
}
