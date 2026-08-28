// Package device tracks transports discovered by the local operating system.
//
// A Registry records discovery independently from terminal Sessions. Discovering
// an endpoint never opens it or creates a Session; callers must make that
// separate, explicit decision.
package device

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultScanInterval  = time.Second
	defaultEventCapacity = 1024
)

var (
	// ErrNilScanner is returned when a Registry cannot enumerate local devices.
	ErrNilScanner = errors.New("device scanner must not be nil")
	// ErrRegistryStarted is returned when Start is called more than once.
	ErrRegistryStarted = errors.New("device registry is already started")
	// ErrRegistryClosed is returned when Start is called after Close.
	ErrRegistryClosed = errors.New("device registry is closed")
	// ErrInvalidReadLimit is returned when an event read requests no events.
	ErrInvalidReadLimit = errors.New("device event read limit must be positive")
)

// State describes whether an endpoint is present in the most recent successful scan.
type State string

const (
	// StatePresent identifies an endpoint reported by the most recent scan.
	StatePresent State = "present"
	// StateDisappeared identifies an endpoint removed by a later successful scan.
	StateDisappeared State = "disappeared"
)

// EventType identifies a discovery state transition.
type EventType string

const (
	// EventAppeared records an endpoint first seen after Registry initialization,
	// or seen again after it disappeared.
	EventAppeared EventType = "appeared"
	// EventDisappeared records an endpoint absent from a successful later scan.
	EventDisappeared EventType = "disappeared"
)

// Endpoint is the minimal, runtime-only identity produced by a Scanner.
//
// Transport names the transport family, such as "serial". Endpoint is the
// platform endpoint accepted by that transport, such as "COM8" or
// "/dev/ttyUSB0". Neither field identifies a physical device across runs.
type Endpoint struct {
	Transport string
	Endpoint  string
	Metadata  SerialMetadata
}

// SerialMetadata is best-effort USB and hardware information for a serial
// endpoint. Empty fields are normal for non-USB controllers and devices that
// omit optional descriptors. VID and PID use four lowercase hexadecimal digits
// when present; this metadata is not a stable device identity.
type SerialMetadata struct {
	VID          string
	PID          string
	USBSerial    string
	Manufacturer string
	Product      string
	USBPath      string
}

// Device is the Registry record for an endpoint discovered during this process.
//
// FirstSeen is retained for the process lifetime. LastSeen is refreshed by each
// successful scan that reports the endpoint. Current listings contain only
// StatePresent devices; disappeared records are retained internally so a later
// reappearance can produce a new EventAppeared transition.
type Device struct {
	DeviceID       string
	IdentityMethod IdentityMethod
	Persistent     bool
	Transport      string
	Endpoint       string
	State          State
	Metadata       SerialMetadata
	FirstSeen      time.Time
	LastSeen       time.Time
}

// Event records one device-presence transition after Registry initialization.
type Event struct {
	Timestamp time.Time
	Type      EventType
	Transport string
	Endpoint  string
}

// Cursor identifies the next device event expected by a consumer.
type Cursor uint64

// EventChunk is a bounded, caller-owned event snapshot.
type EventChunk struct {
	Events  []Event
	Next    Cursor
	Dropped bool
}

// Scanner enumerates endpoints without opening them.
//
// ctx cancellation or deadline must stop the scan where the platform API makes
// that possible. A failed scan does not change Registry presence state, avoiding
// false disappearance events from temporary enumeration failures.
type Scanner interface {
	Scan(ctx context.Context) ([]Endpoint, error)
}

// ScannerFunc adapts a function to Scanner.
type ScannerFunc func(context.Context) ([]Endpoint, error)

// Scan enumerates endpoints through f.
func (f ScannerFunc) Scan(ctx context.Context) ([]Endpoint, error) {
	return f(ctx)
}

// Registry stores local device presence and a bounded event stream.
//
// Start establishes its initial scan as a baseline: endpoints present at startup
// are listed but deliberately do not produce appeared events. Every later
// successful scan emits only state transitions. Registry has no dependency on
// Session, Transport opening, or connection configuration.
type Registry struct {
	scanner  Scanner
	interval time.Duration
	now      func() time.Time
	state    *StateStore

	mu          sync.Mutex
	devices     map[string]Device
	initialized bool
	started     bool
	closed      bool
	cancel      context.CancelFunc

	events eventBuffer
}

// NewRegistry creates an unstarted Registry with the normal low-cost scan interval.
func NewRegistry(scanner Scanner) (*Registry, error) {
	return newRegistry(scanner, defaultScanInterval, defaultEventCapacity, time.Now)
}

// NewRegistryWithStateStore creates an unstarted Registry that assigns device
// identities through store. The Store must already have loaded valid state;
// failure to persist a newly discovered stable identity makes that scan fail so
// callers do not observe an identity that cannot survive a Core restart.
func NewRegistryWithStateStore(scanner Scanner, store *StateStore) (*Registry, error) {
	if store == nil {
		return nil, errors.New("device state store must not be nil")
	}
	registry, err := newRegistry(scanner, defaultScanInterval, defaultEventCapacity, time.Now)
	if err != nil {
		return nil, err
	}
	registry.state = store
	return registry, nil
}

// newRegistry supplies test-controlled scheduling and timestamps without
// exposing runtime configuration that v0.1 does not support.
func newRegistry(scanner Scanner, interval time.Duration, eventCapacity int, now func() time.Time) (*Registry, error) {
	if scanner == nil {
		return nil, ErrNilScanner
	}
	if interval <= 0 {
		return nil, errors.New("device scan interval must be positive")
	}
	if eventCapacity <= 0 {
		return nil, errors.New("device event capacity must be positive")
	}
	if now == nil {
		return nil, errors.New("device clock must not be nil")
	}
	return &Registry{
		scanner:  scanner,
		interval: interval,
		now:      now,
		devices:  make(map[string]Device),
		events:   newEventBuffer(eventCapacity),
	}, nil
}

// Start establishes the initial presence baseline and starts periodic scans.
//
// ctx controls Registry lifetime. Its cancellation stops background scans and
// wakes pending Read calls with io.EOF after retained events are consumed. A
// failed initial scan is returned and leaves the Registry unstarted, so callers
// never mistake an incomplete baseline for confirmed device presence.
func (r *Registry) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrRegistryClosed
	}
	if r.started {
		r.mu.Unlock()
		return ErrRegistryStarted
	}
	r.mu.Unlock()

	if err := r.scan(ctx); err != nil {
		return err
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrRegistryClosed
	}
	if r.started {
		r.mu.Unlock()
		return ErrRegistryStarted
	}
	watchCtx, cancel := context.WithCancel(ctx)
	r.started = true
	r.cancel = cancel
	r.mu.Unlock()
	go r.watch(watchCtx)
	return nil
}

// Close stops periodic scanning and releases pending event readers.
func (r *Registry) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	cancel := r.cancel
	r.cancel = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.events.close()
}

// List returns currently present devices in stable transport and endpoint order.
func (r *Registry) List() []Device {
	r.mu.Lock()
	deferred := make([]Device, 0, len(r.devices))
	for _, device := range r.devices {
		if device.State == StatePresent {
			deferred = append(deferred, device)
		}
	}
	r.mu.Unlock()
	sort.Slice(deferred, func(i, j int) bool {
		if deferred[i].Transport == deferred[j].Transport {
			return deferred[i].Endpoint < deferred[j].Endpoint
		}
		return deferred[i].Transport < deferred[j].Transport
	})
	return deferred
}

// Read waits for device events from next, returning at most limit events.
//
// ctx cancellation or deadline ends the wait promptly. Dropped is true when
// next predates the bounded event retention window; callers should resume from
// Next without assuming they observed every older transition.
func (r *Registry) Read(ctx context.Context, next Cursor, limit int) (EventChunk, error) {
	return r.events.read(ctx, next, limit)
}

// ReadRecent returns the newest retained device events without waiting.
func (r *Registry) ReadRecent(limit int) (EventChunk, error) {
	return r.events.readRecent(limit)
}

// watch continues scanning until the Registry lifetime context ends.
func (r *Registry) watch(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	defer r.events.close()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A transient scan failure intentionally preserves the last known
			// state. The next successful scan will compare against it.
			_ = r.scan(ctx)
		}
	}
}

// scan compares one successful enumeration with the previous successful scan.
func (r *Registry) scan(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	endpoints, err := r.scanner.Scan(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	seen := make(map[string]Endpoint, len(endpoints))
	for _, endpoint := range endpoints {
		endpoint.Transport = strings.TrimSpace(endpoint.Transport)
		endpoint.Endpoint = strings.TrimSpace(endpoint.Endpoint)
		if endpoint.Transport == "" || endpoint.Endpoint == "" {
			return errors.New("device scan returned an endpoint with empty transport or endpoint")
		}
		seen[deviceKey(endpoint)] = endpoint
	}

	now := r.now()
	identities := make(map[string]Identity)
	if r.state != nil {
		identities, err = r.state.Resolve(endpoints, now)
		if err != nil {
			return fmt.Errorf("resolve device identities: %w", err)
		}
	}
	events := make([]Event, 0)
	r.mu.Lock()
	if !r.initialized {
		for key, endpoint := range seen {
			r.devices[key] = newDevice(endpoint, identities[key], now)
		}
		r.initialized = true
		r.mu.Unlock()
		return nil
	}
	for key, endpoint := range seen {
		device, exists := r.devices[key]
		if !exists {
			device = newDevice(endpoint, identities[key], now)
			events = append(events, Event{Timestamp: now, Type: EventAppeared, Transport: endpoint.Transport, Endpoint: endpoint.Endpoint})
		} else {
			if device.State == StateDisappeared {
				events = append(events, Event{Timestamp: now, Type: EventAppeared, Transport: endpoint.Transport, Endpoint: endpoint.Endpoint})
			}
			device.State = StatePresent
			device.Metadata = endpoint.Metadata
			if identity, ok := identities[key]; ok {
				device.DeviceID = identity.DeviceID
				device.IdentityMethod = identity.Method
				device.Persistent = identity.Persistent
			}
			device.LastSeen = now
		}
		r.devices[key] = device
	}
	for key, device := range r.devices {
		if device.State == StatePresent {
			if _, stillPresent := seen[key]; !stillPresent {
				device.State = StateDisappeared
				r.devices[key] = device
				events = append(events, Event{Timestamp: now, Type: EventDisappeared, Transport: device.Transport, Endpoint: device.Endpoint})
			}
		}
	}
	r.mu.Unlock()
	for _, event := range events {
		r.events.append(event)
	}
	return nil
}

// newDevice converts an endpoint and its State Store result into a process
// record. Without a Store, the zero-valued identity fields preserve the
// Registry's standalone behavior for callers that only need discovery.
func newDevice(endpoint Endpoint, identity Identity, now time.Time) Device {
	return Device{
		DeviceID:       identity.DeviceID,
		IdentityMethod: identity.Method,
		Persistent:     identity.Persistent,
		Transport:      endpoint.Transport,
		Endpoint:       endpoint.Endpoint,
		State:          StatePresent,
		Metadata:       endpoint.Metadata,
		FirstSeen:      now,
		LastSeen:       now,
	}
}

// deviceKey creates a collision-free key because endpoints can contain colons.
func deviceKey(endpoint Endpoint) string {
	return endpoint.Transport + "\x00" + endpoint.Endpoint
}

// eventBuffer retains discovery events without allowing slow consumers to block scans.
type eventBuffer struct {
	mu     sync.Mutex
	events []Event
	start  int
	size   int
	base   Cursor
	notify chan struct{}
	closed bool
}

// newEventBuffer allocates fixed event retention for one Registry lifetime.
func newEventBuffer(capacity int) eventBuffer {
	return eventBuffer{events: make([]Event, capacity), notify: make(chan struct{})}
}

// append records an already completed state transition.
func (b *eventBuffer) append(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	if b.size == len(b.events) {
		b.events[b.start] = event
		b.start = (b.start + 1) % len(b.events)
		b.base++
	} else {
		b.events[(b.start+b.size)%len(b.events)] = event
		b.size++
	}
	b.signalLocked()
}

// read waits without holding the buffer lock, preventing readers from blocking scans.
func (b *eventBuffer) read(ctx context.Context, next Cursor, limit int) (EventChunk, error) {
	if limit <= 0 {
		return EventChunk{}, ErrInvalidReadLimit
	}
	for {
		chunk, notify, closed := b.readChunk(next, limit)
		if len(chunk.Events) > 0 || chunk.Dropped {
			return chunk, nil
		}
		if closed {
			return EventChunk{}, io.EOF
		}
		select {
		case <-ctx.Done():
			return EventChunk{}, ctx.Err()
		case <-notify:
		}
	}
}

// readRecent returns an immediate newest-event snapshot and its continuation cursor.
func (b *eventBuffer) readRecent(limit int) (EventChunk, error) {
	if limit <= 0 {
		return EventChunk{}, ErrInvalidReadLimit
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	count := min(limit, b.size)
	return b.copyLocked(b.base+Cursor(b.size-count), count, false), nil
}

// readChunk captures both the current state and its notification generation.
func (b *eventBuffer) readChunk(next Cursor, limit int) (EventChunk, <-chan struct{}, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	dropped := next < b.base
	if dropped {
		next = b.base
	}
	end := b.base + Cursor(b.size)
	if next < end {
		return b.copyLocked(next, min(limit, int(end-next)), dropped), nil, false
	}
	return EventChunk{}, b.notify, b.closed
}

// copyLocked returns a caller-owned linear copy from circular storage.
func (b *eventBuffer) copyLocked(start Cursor, count int, dropped bool) EventChunk {
	events := make([]Event, count)
	for index := range events {
		events[index] = b.events[(b.start+int(start-b.base)+index)%len(b.events)]
	}
	return EventChunk{Events: events, Next: start + Cursor(count), Dropped: dropped}
}

// close wakes every current reader while retaining events still available by cursor.
func (b *eventBuffer) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	b.signalLocked()
}

// signalLocked replaces the notification generation after every state change.
func (b *eventBuffer) signalLocked() {
	close(b.notify)
	b.notify = make(chan struct{})
}
