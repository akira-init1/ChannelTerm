package device

import "time"

// ScanState describes the freshness of the Registry's last successful enumeration.
type ScanState string

const (
	// ScanUnknown means no scan has succeeded yet.
	ScanUnknown ScanState = "unknown"
	// ScanCurrent means the last scan succeeded within three scan intervals.
	ScanCurrent ScanState = "current"
	// ScanFailed means the latest scan failed; retained devices are not current evidence.
	ScanFailed ScanState = "failed"
	// ScanStale means scanning stopped or its last success is older than three intervals.
	ScanStale ScanState = "stale"
)

// DiscoverySnapshot pairs a caller-owned presence list with its scan health.
// Failure retains the previous list without inventing disappearance events.
// LastSuccessfulScan is zero until the first successful scan. A current snapshot
// reports enumeration only, not OS access, exclusive availability, or future identity.
type DiscoverySnapshot struct {
	Devices            []Device
	State              ScanState
	LastSuccessfulScan time.Time
}

// Snapshot copies devices and scan health atomically without triggering discovery.
// Freshness uses the Registry clock, not a remote client's wall clock. Closed
// registries are stale even when their final successful scan was recent.
func (r *Registry) Snapshot() DiscoverySnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := ScanCurrent
	switch {
	case r.closed:
		state = ScanStale
	case r.scanFailed:
		state = ScanFailed
	case !r.initialized:
		state = ScanUnknown
	case r.now().Sub(r.lastSuccessfulScan) > 3*r.interval:
		state = ScanStale
	}
	return DiscoverySnapshot{Devices: r.listLocked(), State: state, LastSuccessfulScan: r.lastSuccessfulScan}
}
