package serial

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	goserial "go.bug.st/serial"
)

func TestDiagnoseOpenErrorClassifiesCommonFailures(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		port     string
		source   error
		want     string
	}{
		{
			name:     "missing Linux device",
			platform: "linux",
			port:     "/dev/ttyUSB0",
			source:   fs.ErrNotExist,
			want:     "does not exist or may have been disconnected",
		},
		{
			name:     "missing Windows port",
			platform: "windows",
			port:     "COM8",
			source:   fs.ErrNotExist,
			want:     "does not exist or may have been disconnected",
		},
		{
			name:     "missing macOS device",
			platform: platformMacOS,
			port:     "/dev/cu.usbserial-110",
			source:   fs.ErrNotExist,
			want:     "does not exist or may have been disconnected",
		},
		{
			name:     "Linux permission denied",
			platform: "linux",
			port:     "/dev/ttyUSB0",
			source:   fs.ErrPermission,
			want:     "check device permissions and that your user is in the dialout group",
		},
		{
			name:     "macOS permission denied",
			platform: platformMacOS,
			port:     "/dev/cu.usbserial-110",
			source:   fs.ErrPermission,
			want:     "check the device permissions in /dev and reconnect the device",
		},
		{
			name:     "macOS busy",
			platform: platformMacOS,
			port:     "/dev/cu.usbserial-110",
			source:   errors.New("resource busy"),
			want:     "may be in use by another program",
		},
		{
			name:     "Windows access denied",
			platform: "windows",
			port:     "COM8",
			source:   errors.New("Access is denied."),
			want:     "may be in use by another program",
		},
		{
			name:     "Windows busy",
			platform: "windows",
			port:     "COM8",
			source:   errors.New("The process cannot access the file because it is being used by another process."),
			want:     "may be in use by another program",
		},
		{
			name:     "unclassified failure",
			platform: "windows",
			port:     "COM8",
			source:   errors.New("driver configuration failed"),
			want:     "open serial port \"COM8\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := diagnoseOpenError(tt.platform, tt.port, tt.source)
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("diagnoseOpenError() error = %q, want %q", err, tt.want)
			}
			if !errors.Is(err, tt.source) {
				t.Errorf("diagnoseOpenError() error = %v, does not retain source error %v", err, tt.source)
			}
		})
	}
}

func TestNewAcceptsMacOSPortNames(t *testing.T) {
	for _, port := range []string{
		"/dev/cu.usbserial-110",
		"/dev/cu.usbmodem14101",
		"/dev/tty.usbserial-110",
		"/dev/tty.usbmodem14101",
	} {
		t.Run(port, func(t *testing.T) {
			if _, err := New(Config{Port: port, BaudRate: 115200}); err != nil {
				t.Errorf("New() error = %v, want macOS port name to be accepted", err)
			}
		})
	}
}

func TestDiagnoseOpenErrorPreservesErrorChain(t *testing.T) {
	source := &fs.PathError{Op: "open", Path: "/dev/cu.usbserial-110", Err: fs.ErrPermission}
	err := diagnoseOpenError(platformMacOS, "/dev/cu.usbserial-110", source)
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("diagnoseOpenError() error = %v, want errors.Is(err, fs.ErrPermission)", err)
	}
	var pathError *fs.PathError
	if !errors.As(err, &pathError) {
		t.Fatalf("diagnoseOpenError() error = %v, want errors.As(err, *fs.PathError)", err)
	}
	if pathError != source {
		t.Errorf("errors.As() path error = %p, want original error %p", pathError, source)
	}
}

func TestTransportConnectPreservesOpenError(t *testing.T) {
	transport, err := New(Config{Port: "COM8", BaudRate: 115200})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	source := errors.New("Access is denied.")
	transport.open = func(string, *goserial.Mode) (goserial.Port, error) {
		return nil, source
	}

	err = transport.Connect(context.Background())
	if !errors.Is(err, source) {
		t.Errorf("Connect() error = %v, does not retain source error %v", err, source)
	}
}

func TestNewValidatesConfigAndAppliesDefaults(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr error
	}{
		{name: "missing port", config: Config{BaudRate: 115200}, wantErr: ErrInvalidPortName},
		{name: "invalid baud rate", config: Config{Port: "COM3"}, wantErr: ErrInvalidBaudRate},
		{name: "invalid data bits", config: Config{Port: "COM3", BaudRate: 115200, DataBits: 4}, wantErr: ErrInvalidDataBits},
		{name: "invalid parity", config: Config{Port: "COM3", BaudRate: 115200, Parity: "bad"}, wantErr: ErrInvalidParity},
		{name: "invalid stop bits", config: Config{Port: "COM3", BaudRate: 115200, StopBits: "3"}, wantErr: ErrInvalidStopBits},
		{name: "invalid flow control", config: Config{Port: "COM3", BaudRate: 115200, FlowControl: "invalid"}, wantErr: ErrInvalidFlowControl},
		{name: "software flow control unsupported", config: Config{Port: "COM3", BaudRate: 115200, FlowControl: FlowControlSoftware}, wantErr: ErrFlowControlUnsupported},
		{name: "hardware flow control unsupported", config: Config{Port: "COM3", BaudRate: 115200, FlowControl: FlowControlHardware}, wantErr: ErrFlowControlUnsupported},
		{name: "defaults", config: Config{Port: "COM3", BaudRate: 115200}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, err := New(tt.config)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("New() error = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if transport.config.DataBits != 8 || transport.config.Parity != ParityNone || transport.config.StopBits != StopBitsOne || transport.config.FlowControl != FlowControlNone {
				t.Errorf("New() config = %+v, want 8-N-1 defaults with no flow control", transport.config)
			}
		})
	}
}

func TestTransportConnectReadWriteAndClose(t *testing.T) {
	transport, err := New(Config{
		Port:     "COM3",
		BaudRate: 115200,
		DataBits: 7,
		Parity:   ParityEven,
		StopBits: StopBitsTwo,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	port := newFakePort()
	var gotName string
	var gotMode *goserial.Mode
	transport.open = func(name string, mode *goserial.Mode) (goserial.Port, error) {
		gotName = name
		gotMode = mode
		return port, nil
	}

	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if gotName != "COM3" {
		t.Errorf("open name = %q, want COM3", gotName)
	}
	if gotMode.BaudRate != 115200 || gotMode.DataBits != 7 || gotMode.Parity != goserial.EvenParity || gotMode.StopBits != goserial.TwoStopBits {
		t.Errorf("open mode = %+v, want configured mode", gotMode)
	}

	port.enqueueRead([]byte("ready"))
	data := make([]byte, 16)
	n, err := transport.Read(data)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got := string(data[:n]); got != "ready" {
		t.Errorf("Read() = %q, want ready", got)
	}
	if n, err := transport.Write([]byte("status\n")); err != nil || n != len("status\n") {
		t.Errorf("Write() = (%d, %v), want (%d, nil)", n, err, len("status\n"))
	}
	if got := string(port.writtenData()); got != "status\n" {
		t.Errorf("port writes = %q, want status\\n", got)
	}
	if err := transport.Resize(80, 24); !errors.Is(err, ErrResizeUnsupported) {
		t.Errorf("Resize() error = %v, want ErrResizeUnsupported", err)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := port.closeCount(); got != 1 {
		t.Errorf("underlying Close() calls = %d, want 1", got)
	}
	if _, err := transport.Read(data); !errors.Is(err, ErrNotOpen) {
		t.Errorf("Read() after Close error = %v, want ErrNotOpen", err)
	}
}

func TestTransportCloseUnblocksRead(t *testing.T) {
	transport, err := New(Config{Port: "COM3", BaudRate: 115200})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	port := newFakePort()
	transport.open = func(string, *goserial.Mode) (goserial.Port, error) { return port, nil }
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	readDone := make(chan error, 1)
	go func() {
		_, err := transport.Read(make([]byte, 1))
		readDone <- err
	}()
	select {
	case <-port.readStarted:
	case <-time.After(time.Second):
		t.Fatal("Read() did not begin")
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-readDone:
		if !errors.Is(err, io.EOF) {
			t.Errorf("blocked Read() error = %v, want io.EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not unblock Read()")
	}
}

func TestTransportConnectClosesPortWhenContextCancelsDuringOpen(t *testing.T) {
	transport, err := New(Config{Port: "COM3", BaudRate: 115200})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	port := newFakePort()
	ctx, cancel := context.WithCancel(context.Background())
	transport.open = func(string, *goserial.Mode) (goserial.Port, error) {
		cancel()
		return port, nil
	}
	if err := transport.Connect(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Connect() error = %v, want context.Canceled", err)
	}
	if got := port.closeCount(); got != 1 {
		t.Errorf("underlying Close() calls = %d, want 1", got)
	}
}

func TestTransportCanOpenAgainAfterClose(t *testing.T) {
	transport, err := New(Config{Port: "COM3", BaudRate: 115200})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	first := newFakePort()
	second := newFakePort()
	ports := []goserial.Port{first, second}
	openCount := 0
	transport.open = func(string, *goserial.Mode) (goserial.Port, error) {
		port := ports[openCount]
		openCount++
		return port, nil
	}

	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect() error = %v", err)
	}
	if _, err := transport.Write([]byte("reopened")); err != nil {
		t.Fatalf("Write() after reopen error = %v", err)
	}
	if got := string(second.writtenData()); got != "reopened" {
		t.Errorf("second port writes = %q, want reopened", got)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if first.closeCount() != 1 || second.closeCount() != 1 {
		t.Errorf("port close counts = %d/%d, want 1/1", first.closeCount(), second.closeCount())
	}
}

// fakePort models the serial library's blocking Read and idempotent Close
// behavior so Transport tests can verify lifecycle guarantees without hardware.
type fakePort struct {
	mu sync.Mutex

	readQueue   chan []byte
	readStarted chan struct{}
	closed      chan struct{}
	closeOnce   sync.Once

	written []byte
	closes  int
}

// newFakePort gives each test isolated read and close notifications.
func newFakePort() *fakePort {
	return &fakePort{
		readQueue:   make(chan []byte, 1),
		readStarted: make(chan struct{}),
		closed:      make(chan struct{}),
	}
}

func (p *fakePort) SetMode(*goserial.Mode) error { return nil }

func (p *fakePort) Read(data []byte) (int, error) {
	p.closeReadStarted()
	select {
	case queued := <-p.readQueue:
		return copy(data, queued), nil
	case <-p.closed:
		return 0, io.EOF
	}
}

func (p *fakePort) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.written = append(p.written, data...)
	return len(data), nil
}

func (p *fakePort) Drain() error             { return nil }
func (p *fakePort) ResetInputBuffer() error  { return nil }
func (p *fakePort) ResetOutputBuffer() error { return nil }
func (p *fakePort) SetDTR(bool) error        { return nil }
func (p *fakePort) SetRTS(bool) error        { return nil }
func (p *fakePort) GetModemStatusBits() (*goserial.ModemStatusBits, error) {
	return &goserial.ModemStatusBits{}, nil
}
func (p *fakePort) SetReadTimeout(time.Duration) error { return nil }

func (p *fakePort) Close() error {
	p.mu.Lock()
	p.closes++
	p.mu.Unlock()
	p.closeOnce.Do(func() { close(p.closed) })
	return nil
}

func (p *fakePort) Break(time.Duration) error { return nil }

func (p *fakePort) enqueueRead(data []byte) {
	p.readQueue <- append([]byte(nil), data...)
}

func (p *fakePort) closeReadStarted() {
	select {
	case <-p.readStarted:
	default:
		close(p.readStarted)
	}
}

func (p *fakePort) writtenData() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.written...)
}

func (p *fakePort) closeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closes
}
