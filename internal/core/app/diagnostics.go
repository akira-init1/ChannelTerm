package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"sort"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

const (
	diagnosticRawLimit     = 2 * 1024 * 1024
	diagnosticArchiveLimit = 16 * 1024 * 1024
	diagnosticCountLimit   = 4
	diagnosticTTL          = 15 * time.Minute
	diagnosticReadLimit    = 64 * 1024
)

// ErrDiagnosticNotFound covers unknown, evicted and expired archive IDs.
var ErrDiagnosticNotFound = errors.New("diagnostic archive not found or expired")

// BuildProvenance identifies the Host executable, independently of adapters.
type BuildProvenance struct {
	Version string
	Commit  string
	Time    string
}

// DiagnosticArchive describes an immutable, Host-local archive. It is retained
// in bounded memory until eviction/expiry or Host exit, never at a remote path.
type DiagnosticArchive struct {
	ID        string    `json:"diagnostic_id"`
	Size      int       `json:"size"`
	SHA256    string    `json:"sha256"`
	ExpiresAt time.Time `json:"expires_at"`
}

type diagnosticEntry struct {
	info DiagnosticArchive
	data []byte
}

// diagnosticStore bounds both concurrent archive construction and retained
// archives. Expired bytes are reclaimed on the next store access; reads reject
// expired IDs immediately. No filesystem paths or additional listeners exist.
type diagnosticStore struct {
	mu      sync.Mutex
	entries []diagnosticEntry
}

type diagnosticSegment struct {
	Offset     int        `json:"file_offset"`
	Start      uint64     `json:"output_start"`
	End        uint64     `json:"output_end"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	Dropped    bool       `json:"dropped"`
}

// diagnosticCapture records application observer times, never invented UART arrival
// times. Byte and segment limits bound memory even for a very noisy terminal.
type diagnosticCapture struct {
	data      []byte
	segments  []diagnosticSegment
	truncated bool
}

func (c *diagnosticCapture) append(chunk session.OutputChunk) {
	n := min(len(chunk.Data), diagnosticRawLimit-len(c.data))
	if n != len(chunk.Data) || len(c.segments) >= 4096 {
		c.truncated = true
	}
	if len(c.segments) >= 4096 || n == 0 {
		return
	}
	now := time.Now().UTC()
	start := uint64(chunk.Next) - uint64(len(chunk.Data))
	c.segments = append(c.segments, diagnosticSegment{Offset: len(c.data), Start: start, End: start + uint64(n), ObservedAt: &now, Dropped: chunk.Dropped})
	c.data = append(c.data, chunk.Data[:n]...)
}

func (a *Application) diagnosticSessionInfo(id string) session.SessionInfo {
	for _, info := range a.ListSessions() {
		if info.ID == id {
			return info
		}
	}
	return session.SessionInfo{ID: id}
}

// ExportDiagnostics exports independently sampled retained buffers, not an atomic
// full-session recording. includeRaw explicitly permits terminal contents and
// write bytes. Unknown serial settings and missing history remain explicit.
func (a *Application) ExportDiagnostics(ctx context.Context, identifier string, includeRaw bool) (DiagnosticArchive, error) {
	if err := ctx.Err(); err != nil {
		return DiagnosticArchive{}, err
	}
	t, err := a.session(identifier)
	if err != nil {
		return DiagnosticArchive{}, err
	}
	a.diagnostics.mu.Lock()
	defer a.diagnostics.mu.Unlock()
	c := &diagnosticCapture{}
	if includeRaw {
		chunk, err := t.ReadRecent(diagnosticRawLimit)
		if err != nil {
			return DiagnosticArchive{}, err
		}
		c.data = chunk.Data
		start := uint64(chunk.Next) - uint64(len(chunk.Data))
		c.truncated = start > 0 || chunk.Dropped
		c.segments = []diagnosticSegment{{Start: start, End: uint64(chunk.Next), Dropped: chunk.Dropped}}
	}
	return a.buildDiagnosticLocked(ctx, t, a.diagnosticSessionInfo(t.ID()), c, includeRaw, "snapshot", nil, nil)
}

// ReadDiagnostic returns a copied, bounded archive slice. IDs cannot be paths;
// access uses the existing Host authorization and is shared by its clients.
func (a *Application) ReadDiagnostic(ctx context.Context, id string, offset, limit int) ([]byte, int, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, false, err
	}
	if offset < 0 || limit < 1 || limit > diagnosticReadLimit {
		return nil, 0, false, errors.New("diagnostic offset must be nonnegative and max_bytes must be 1..65536")
	}
	a.diagnostics.mu.Lock()
	defer a.diagnostics.mu.Unlock()
	a.diagnostics.prune(time.Now())
	for _, e := range a.diagnostics.entries {
		if e.info.ID != id {
			continue
		}
		if offset > len(e.data) {
			return nil, 0, false, errors.New("diagnostic offset exceeds archive size")
		}
		end := offset + min(limit, len(e.data)-offset)
		return append([]byte(nil), e.data[offset:end]...), end, end == len(e.data), nil
	}
	return nil, 0, false, ErrDiagnosticNotFound
}

// DiagnosticInfo returns immutable download metadata without exposing a path.
func (a *Application) DiagnosticInfo(id string) (DiagnosticArchive, error) {
	a.diagnostics.mu.Lock()
	defer a.diagnostics.mu.Unlock()
	a.diagnostics.prune(time.Now())
	for _, e := range a.diagnostics.entries {
		if e.info.ID == id {
			return e.info, nil
		}
	}
	return DiagnosticArchive{}, ErrDiagnosticNotFound
}

func (s *diagnosticStore) prune(now time.Time) {
	kept := s.entries[:0]
	for _, e := range s.entries {
		if now.Before(e.info.ExpiresAt) {
			kept = append(kept, e)
		}
	}
	clear(s.entries[len(kept):])
	s.entries = kept
}

func (a *Application) buildDiagnosticLocked(ctx context.Context, t session.Session, info session.SessionInfo, c *diagnosticCapture, raw bool, mode string, operation any, failures []map[string]any) (DiagnosticArchive, error) {
	// A disconnected Session can no longer serve retained metadata. Preserve
	// already captured raw evidence and explicitly mark those buffers missing.
	activity, activityErr := t.ReadRecentActivity(256)
	events, eventsErr := t.ReadRecentEvents(256)
	var err error
	files := map[string][]byte{}
	manifest := map[string]any{
		"format_version": 1, "created_at": time.Now().UTC(), "mode": mode, "raw_included": raw,
		"session_id": t.ID(), "session_ref": info.Metadata.Reference, "transport": info.Metadata.Transport,
		"endpoint": info.Metadata.Endpoint, "host_os": runtime.GOOS, "host_arch": runtime.GOARCH,
		"version": "unknown", "commit": "unknown", "build_time": "unknown",
		"serial_settings": map[string]any{"baud": "unknown", "data_bits": "unknown", "parity": "unknown", "stop_bits": "unknown", "flow_control": "unknown"},
		"output_segments": c.segments, "output_truncated": c.truncated, "time_source": "application_observation_not_uart_arrival",
		"activity_next": activity.Next, "activity_retained_start": uint64(activity.Next) - uint64(len(activity.Events)),
		"events_next": events.Next, "events_retained_start": uint64(events.Next) - uint64(len(events.Events)),
		"activity_dropped": activity.Dropped, "events_dropped": events.Dropped,
		"buffers_sampled_independently": true, "historical_errors_available": false,
		"activity_unavailable": activityErr != nil, "events_unavailable": eventsErr != nil,
	}
	if build, ok := debug.ReadBuildInfo(); ok {
		manifest["version"] = build.Main.Version
		for _, setting := range build.Settings {
			if setting.Key == "vcs.revision" {
				manifest["commit"] = setting.Value
			}
			if setting.Key == "vcs.time" {
				manifest["source_commit_time"] = setting.Value
			}
		}
	}
	if a.build.Version != "" {
		manifest["version"] = a.build.Version
	}
	if a.build.Commit != "" {
		manifest["commit"] = a.build.Commit
	}
	if a.build.Time != "" {
		manifest["build_time"] = a.build.Time
	}
	if raw {
		files["terminal.bin"] = c.data
		files["terminal.hex"] = []byte(hex.EncodeToString(c.data) + "\n")
		files["terminal.log"] = diagnosticText(c.data)
		if mode == "operation" {
			files["rx-index.jsonl"] = jsonLines(c.segments)
		}
	}
	activityRecords := make([]map[string]any, 0, len(activity.Events))
	remaining := 256 * 1024
	for i, e := range activity.Events {
		record := map[string]any{"cursor": uint64(activity.Next) - uint64(len(activity.Events)) + uint64(i), "timestamp": e.Timestamp, "actor": e.Actor, "operation": e.Operation, "bytes_written": len(e.Data)}
		if raw {
			n := min(remaining, len(e.Data))
			remaining -= n
			record["data_base64"] = e.Data[:n]
			record["payload_truncated"] = n != len(e.Data)
		}
		activityRecords = append(activityRecords, record)
	}
	files["activity.jsonl"] = jsonLines(activityRecords)
	// Events can contain shell commands and paths. Export only bounded,
	// explicitly permitted metadata, even when raw capture was requested.
	for i := range events.Events {
		safe := map[string]any{}
		for _, key := range []string{"operation_id", "operation", "status", "transfer_id", "command_id", "output_cursor", "transferred", "total", "exit_code"} {
			if v, ok := events.Events[i].Metadata[key]; ok {
				if encoded, e := json.Marshal(v); e == nil && len(encoded) <= 256 {
					safe[key] = v
				}
			}
		}
		events.Events[i].Metadata = safe
	}
	files["events.jsonl"] = jsonLines(events.Events)
	files["errors.jsonl"] = jsonLines(failures)
	if operation != nil {
		manifest["operation"] = operation
	}
	hashes := map[string]string{}
	for name, data := range files {
		sum := sha256.Sum256(data)
		hashes[name] = hex.EncodeToString(sum[:])
	}
	manifest["sha256"] = hashes
	files["manifest.json"], err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return DiagnosticArchive{}, err
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return DiagnosticArchive{}, err
		}
		entry, err := writer.Create(name)
		if err != nil {
			return DiagnosticArchive{}, err
		}
		if _, err = entry.Write(files[name]); err != nil {
			return DiagnosticArchive{}, err
		}
	}
	if err = writer.Close(); err != nil {
		return DiagnosticArchive{}, err
	}
	if archive.Len() > diagnosticArchiveLimit {
		return DiagnosticArchive{}, errors.New("diagnostic archive exceeds 16 MiB")
	}
	token, err := newTerminalCommandToken()
	if err != nil {
		return DiagnosticArchive{}, err
	}
	sum := sha256.Sum256(archive.Bytes())
	meta := DiagnosticArchive{ID: "DIAG-" + token, Size: archive.Len(), SHA256: hex.EncodeToString(sum[:]), ExpiresAt: time.Now().UTC().Add(diagnosticTTL)}
	a.diagnostics.prune(time.Now())
	if len(a.diagnostics.entries) >= diagnosticCountLimit {
		a.diagnostics.entries = append([]diagnosticEntry(nil), a.diagnostics.entries[1:]...)
	}
	a.diagnostics.entries = append(a.diagnostics.entries, diagnosticEntry{info: meta, data: archive.Bytes()})
	return meta, nil
}

func jsonLines[T any](values []T) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, v := range values {
		_ = enc.Encode(v)
	} // All callers supply bounded JSON-safe records.
	return b.Bytes()
}

// diagnosticText escapes control and malformed bytes with file offsets. It is
// explicitly a derived view; terminal.bin remains the authoritative evidence.
func diagnosticText(raw []byte) []byte {
	var b bytes.Buffer
	b.WriteString("Derived UTF-8 view; escapes use terminal.bin byte offsets.\n")
	for offset := 0; offset < len(raw); {
		r, n := utf8.DecodeRune(raw[offset:])
		if r == utf8.RuneError && n == 1 || unicode.IsControl(r) && r != '\n' && r != '\t' {
			for i := 0; i < n; i++ {
				fmt.Fprintf(&b, "<0x%02X@%d>", raw[offset+i], offset+i)
			}
		} else {
			b.Write(raw[offset : offset+n])
		}
		offset += n
	}
	return b.Bytes()
}
