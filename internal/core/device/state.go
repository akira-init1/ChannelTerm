package device

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const stateVersion = 1

var (
	// ErrUnsupportedStateVersion is returned when a state file requires a
	// migration that this Core does not implement. The file is left unchanged.
	ErrUnsupportedStateVersion = errors.New("unsupported state version")
)

// IdentityMethod explains the evidence used to assign a DeviceID.
//
// USBSerial is device-level when the adapter supplies a serial number. USBPath
// is only location-level: it identifies a class of device at one physical USB
// position. Runtime has no durable matching evidence and is process-local.
type IdentityMethod string

const (
	// IdentityUSBSerial identifies an adapter using transport, VID, PID, and a
	// non-empty USB serial number.
	IdentityUSBSerial IdentityMethod = "usb_serial"
	// IdentityUSBPath identifies an adapter location using transport, VID, PID,
	// and USB path only when the adapter has no USB serial number.
	IdentityUSBPath IdentityMethod = "usb_path"
	// IdentityRuntime identifies an endpoint that lacks enough metadata for a
	// durable identity. Its DeviceID is deliberately not written to state.json.
	IdentityRuntime IdentityMethod = "runtime"
)

// Identity is the identity result exposed by the Device Registry.
type Identity struct {
	DeviceID   string
	Method     IdentityMethod
	Persistent bool
}

// StateIdentity is the matching evidence persisted for one managed device.
// Manufacturer and Product are diagnostic snapshots only; neither participates
// in matching because descriptions are not reliable unique identifiers.
type StateIdentity struct {
	VID          string `json:"vid"`
	PID          string `json:"pid"`
	USBSerial    string `json:"usb_serial"`
	USBPath      string `json:"usb_path"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Product      string `json:"product,omitempty"`
}

// StateDevice is one ChannelTerm-managed durable device identity.
//
// LastEndpoint is an observation for display and diagnostics. It never takes
// part in matching, because COM and tty names may be reassigned by the OS.
type StateDevice struct {
	DeviceID     string        `json:"device_id"`
	Transport    string        `json:"transport"`
	Identity     StateIdentity `json:"identity"`
	LastEndpoint string        `json:"last_endpoint"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// StateFile is the versioned JSON schema stored in state.json.
type StateFile struct {
	Version int           `json:"version"`
	Devices []StateDevice `json:"devices"`
}

// StateStore owns ChannelTerm's local durable device identity mapping.
//
// Its mutex protects matching and the subsequent atomic replacement so two
// concurrent scans cannot create duplicate records. One Core instance owns the
// file; v0.1 intentionally does not implement a cross-process file lock.
type StateStore struct {
	path string

	mu      sync.Mutex
	state   StateFile
	runtime map[string]Identity
	now     func() time.Time
	newID   func() (string, error)
	write   func(string, []byte) error
}

// LoadStateStore opens path, or creates an empty version-1 state file when it
// does not exist. Invalid JSON and unsupported versions are returned as clear
// errors without changing the original file.
func LoadStateStore(path string) (*StateStore, error) {
	store := newStateStore(path, time.Now, randomDeviceID, atomicWrite)
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

// newStateStore supplies deterministic clocks, ID generation, and replacement
// failures for tests of persistence and atomic-write behavior.
func newStateStore(path string, now func() time.Time, newID func() (string, error), write func(string, []byte) error) *StateStore {
	return &StateStore{
		path:    path,
		state:   StateFile{Version: stateVersion, Devices: []StateDevice{}},
		runtime: make(map[string]Identity),
		now:     now,
		newID:   newID,
		write:   write,
	}
}

// Resolve assigns identities for one successful scan. Persistent state is
// written once only if a durable record is created or its snapshot changes;
// unchanged periodic scans perform no disk writes.
func (s *StateStore) Resolve(endpoints []Endpoint, observedAt time.Time) (map[string]Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if observedAt.IsZero() {
		observedAt = s.now()
	}

	resolved := make(map[string]Identity, len(endpoints))
	used := make(map[string]struct{})
	before := cloneStateFile(s.state)
	committed := false
	defer func() {
		if !committed {
			s.state = before
		}
	}()
	changed := false
	for _, endpoint := range endpoints {
		key := deviceKey(endpoint)
		method, evidence := persistentEvidence(endpoint)
		if method == IdentityRuntime {
			identity, ok := s.runtime[key]
			if !ok {
				id, err := s.allocateID()
				if err != nil {
					return nil, err
				}
				identity = Identity{DeviceID: id, Method: IdentityRuntime, Persistent: false}
				s.runtime[key] = identity
			}
			resolved[key] = identity
			continue
		}

		matches := s.matches(endpoint.Transport, method, evidence)
		alreadyUsed := false
		if len(matches) == 1 {
			_, alreadyUsed = used[s.state.Devices[matches[0]].DeviceID]
		}
		if len(matches) > 1 || alreadyUsed {
			// Multiple records with equal durable evidence, or two live endpoints
			// claiming one record, is ambiguous. Do not merge them by choosing one.
			id, err := s.allocateID()
			if err != nil {
				return nil, err
			}
			resolved[key] = Identity{DeviceID: id, Method: IdentityRuntime, Persistent: false}
			s.runtime[key] = resolved[key]
			continue
		}

		if len(matches) == 1 {
			record := &s.state.Devices[matches[0]]
			used[record.DeviceID] = struct{}{}
			if updateStateRecord(record, endpoint, evidence, observedAt) {
				changed = true
			}
			resolved[key] = Identity{DeviceID: record.DeviceID, Method: method, Persistent: true}
			continue
		}

		id, err := s.allocateID()
		if err != nil {
			return nil, err
		}
		s.state.Devices = append(s.state.Devices, StateDevice{
			DeviceID:     id,
			Transport:    endpoint.Transport,
			Identity:     evidence,
			LastEndpoint: endpoint.Endpoint,
			CreatedAt:    observedAt,
			UpdatedAt:    observedAt,
		})
		used[id] = struct{}{}
		changed = true
		resolved[key] = Identity{DeviceID: id, Method: method, Persistent: true}
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	committed = true
	return resolved, nil
}

// cloneStateFile preserves an in-memory snapshot until its atomic replacement
// succeeds. A failed replacement must not make later scans believe unwritten
// changes already reached durable storage.
func cloneStateFile(file StateFile) StateFile {
	clone := file
	clone.Devices = append([]StateDevice(nil), file.Devices...)
	return clone
}

// load validates existing state before retaining it. It creates an explicitly
// empty v1 file only for a missing path, never as recovery from bad JSON.
func (s *StateStore) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.saveLocked()
	}
	if err != nil {
		return fmt.Errorf("read device state %q: %w", s.path, err)
	}
	var stored StateFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("decode device state %q: %w", s.path, err)
	}
	if stored.Version != stateVersion {
		return fmt.Errorf("%w: %d", ErrUnsupportedStateVersion, stored.Version)
	}
	if stored.Devices == nil {
		stored.Devices = []StateDevice{}
	}
	if err := validateStateFile(stored); err != nil {
		return fmt.Errorf("validate device state %q: %w", s.path, err)
	}
	s.state = stored
	return nil
}

// validateStateFile rejects malformed but syntactically valid records before
// they can cause an empty or duplicate DeviceID to leak into the Registry.
func validateStateFile(file StateFile) error {
	seen := make(map[string]struct{}, len(file.Devices))
	for _, record := range file.Devices {
		if strings.TrimSpace(record.DeviceID) == "" || strings.TrimSpace(record.Transport) == "" {
			return errors.New("device record has empty device_id or transport")
		}
		if record.Identity.VID == "" || record.Identity.PID == "" || (record.Identity.USBSerial == "" && record.Identity.USBPath == "") {
			return fmt.Errorf("device record %q has incomplete persistent identity", record.DeviceID)
		}
		if _, duplicate := seen[record.DeviceID]; duplicate {
			return fmt.Errorf("duplicate device_id %q", record.DeviceID)
		}
		seen[record.DeviceID] = struct{}{}
	}
	return nil
}

// matches returns every durable record whose valid matching evidence equals the
// current endpoint. Returning all matches prevents accidental first-match wins.
func (s *StateStore) matches(transport string, method IdentityMethod, evidence StateIdentity) []int {
	matches := make([]int, 0, 1)
	for index, record := range s.state.Devices {
		if record.Transport != transport {
			continue
		}
		if method == IdentityUSBSerial && record.Identity.USBSerial != "" && record.Identity.VID == evidence.VID && record.Identity.PID == evidence.PID && record.Identity.USBSerial == evidence.USBSerial {
			matches = append(matches, index)
		}
		if method == IdentityUSBPath && record.Identity.USBSerial == "" && record.Identity.VID == evidence.VID && record.Identity.PID == evidence.PID && record.Identity.USBPath == evidence.USBPath {
			matches = append(matches, index)
		}
	}
	return matches
}

// persistentEvidence returns only the evidence allowed by v0.1's conservative
// matcher. A serial-less USB path identifies a location, never the board.
func persistentEvidence(endpoint Endpoint) (IdentityMethod, StateIdentity) {
	evidence := StateIdentity{
		VID:          strings.TrimSpace(endpoint.Metadata.VID),
		PID:          strings.TrimSpace(endpoint.Metadata.PID),
		USBSerial:    strings.TrimSpace(endpoint.Metadata.USBSerial),
		USBPath:      strings.TrimSpace(endpoint.Metadata.USBPath),
		Manufacturer: strings.TrimSpace(endpoint.Metadata.Manufacturer),
		Product:      strings.TrimSpace(endpoint.Metadata.Product),
	}
	if evidence.VID != "" && evidence.PID != "" && evidence.USBSerial != "" {
		return IdentityUSBSerial, evidence
	}
	if evidence.VID != "" && evidence.PID != "" && evidence.USBSerial == "" && evidence.USBPath != "" {
		return IdentityUSBPath, evidence
	}
	return IdentityRuntime, StateIdentity{}
}

// updateStateRecord refreshes diagnostics and endpoint observations after a
// trusted match. It intentionally excludes a serial device's old USB path from
// matching but records the new path as a snapshot after that match succeeds.
func updateStateRecord(record *StateDevice, endpoint Endpoint, evidence StateIdentity, observedAt time.Time) bool {
	if record.Identity == evidence && record.LastEndpoint == endpoint.Endpoint {
		return false
	}
	record.Identity = evidence
	record.LastEndpoint = endpoint.Endpoint
	record.UpdatedAt = observedAt
	return true
}

// allocateID creates an unused ChannelTerm-generated identifier. It does not
// expose endpoint or hardware data, so a DeviceID is never a hardware hash.
func (s *StateStore) allocateID() (string, error) {
	for range 8 {
		id, err := s.newID()
		if err != nil {
			return "", fmt.Errorf("generate device ID: %w", err)
		}
		if !s.hasID(id) {
			return id, nil
		}
	}
	return "", errors.New("generate unique device ID: repeated collision")
}

// hasID checks both durable and current runtime identities before accepting an
// ID. The latter makes process-local IDs distinct from persistent IDs too.
func (s *StateStore) hasID(id string) bool {
	for _, record := range s.state.Devices {
		if record.DeviceID == id {
			return true
		}
	}
	for _, identity := range s.runtime {
		if identity.DeviceID == id {
			return true
		}
	}
	return false
}

// saveLocked serializes a complete versioned snapshot and atomically replaces
// the old file. Callers must hold s.mu whenever state might change.
func (s *StateStore) saveLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode device state %q: %w", s.path, err)
	}
	data = append(data, '\n')
	if err := s.write(s.path, data); err != nil {
		return fmt.Errorf("write device state %q: %w", s.path, err)
	}
	return nil
}

// atomicWrite writes a complete replacement in the destination directory so
// rename does not cross filesystems and an interrupted write leaves the prior
// state.json intact.
func atomicWrite(path string, data []byte) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create device state directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary device state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set temporary device state permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary device state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary device state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary device state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace device state: %w", err)
	}
	return nil
}

// randomDeviceID returns a non-predictable 128-bit identifier using the Go
// standard library's cryptographic random source.
func randomDeviceID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "dev_" + hex.EncodeToString(bytes), nil
}
