package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/device"
)

// fixtureDevices changes enumeration through private test stdin, never physical I/O.
type fixtureDevices struct {
	mu         sync.Mutex
	mode       string
	openReason string
	opens      int
	registry   *device.Registry
}

func newFixtureDevices(directory string) (*fixtureDevices, error) {
	f := &fixtureDevices{}
	store, err := device.LoadStateStore(filepath.Join(directory, "devices.json"))
	if err != nil {
		return nil, err
	}
	f.registry, err = device.NewRegistryWithStateStore(device.ScannerFunc(f.scan), store)
	if err != nil {
		return nil, err
	}
	if err := f.registry.Start(context.Background()); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *fixtureDevices) scan(context.Context) ([]device.Endpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mode == "failed" {
		return nil, fmt.Errorf("fixture enumeration unavailable")
	}
	if f.mode == "absent" {
		return nil, nil
	}
	port := "COM-FIXTURE"
	if f.mode == "moved" {
		port = "COM-MOVED"
	}
	entry := device.Endpoint{Transport: "serial", Endpoint: port, Metadata: device.SerialMetadata{VID: "1234", PID: "5678", USBSerial: "FIXTURE-ONLY", USBPath: "fixture/usb/1", Manufacturer: "Test fixture", Product: "In-memory serial"}}
	entries := []device.Endpoint{entry}
	if f.mode == "ambiguous" {
		entry.Endpoint = "COM-DUPLICATE"
		entries = append(entries, entry)
	}
	return entries, nil
}

// configure waits for the real Registry to publish the requested fixture state.
func (f *fixtureDevices) configure(mode string) error {
	switch mode {
	case "", "moved", "absent", "failed", "ambiguous":
	default:
		return fmt.Errorf("invalid fixture discovery mode")
	}
	before := f.registry.Snapshot().LastSuccessfulScan
	f.mu.Lock()
	f.mode = mode
	f.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := f.registry.Snapshot()
		if mode == "failed" && snapshot.State == device.ScanFailed {
			return nil
		}
		if mode != "failed" && snapshot.State == device.ScanCurrent && snapshot.LastSuccessfulScan.After(before) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("fixture discovery did not update")
}

func (f *fixtureDevices) opening() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	if f.openReason != "" {
		return fixtureSerialError(f.openReason)
	}
	return nil
}

// fixtureSerialError exercises the actual MCP typed-error projection without OS probing.
type fixtureSerialError string

// Error returns non-sensitive fixture diagnostics.
func (e fixtureSerialError) Error() string { return "fixture serial open refused" }

// ErrorCode returns the finite serial reason under test.
func (e fixtureSerialError) ErrorCode() string { return string(e) }
