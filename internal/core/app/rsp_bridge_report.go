package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// BridgeReportRequest is a caller-selected account of external GDB activity.
// Kind is command, result, or state; Text must omit protocol bytes and secrets.
// Sequence increases per bridge; gaps are allowed so failed reporting cannot
// hold up debugging. This is not an execution request or a complete GDB audit.
type BridgeReportRequest struct {
	BridgeID string `json:"bridge_id"`
	Owner    string `json:"owner"`
	Sequence uint64 `json:"sequence"`
	Kind     string `json:"kind"`
	Text     string `json:"text"`
}

// BridgeReportResult acknowledges event publication, not GDB command execution.
type BridgeReportResult struct {
	BridgeID string `json:"bridge_id"`
	Sequence uint64 `json:"sequence"`
}

// ReportRSPBridge publishes Codex-reported text through the Session event stream.
// It never writes target bytes, runs GDB, changes target state, or renews leases.
// Failure/cancellation affects only reporting. The latest identical report may
// be retried without duplication; older/conflicting sequences are rejected.
// Only bounded UTF-8 text is accepted, with known bridge capabilities rejected.
// Callers remain responsible for omitting other secrets and raw RSP traffic.
func (a *Application) ReportRSPBridge(ctx context.Context, q BridgeReportRequest) (BridgeReportResult, error) {
	if err := ctx.Err(); err != nil {
		return BridgeReportResult{}, err
	}
	r, err := a.bridges.get(q.BridgeID, q.Owner)
	if err != nil {
		return BridgeReportResult{}, err
	}
	if q.Kind != "command" && q.Kind != "result" && q.Kind != "state" {
		return BridgeReportResult{}, errors.New("report kind must be command, result, or state")
	}
	if q.Sequence == 0 || q.Sequence > 1<<53-1 || len(q.Text) > 4096 || strings.TrimSpace(q.Text) == "" || !utf8.ValidString(q.Text) {
		return BridgeReportResult{}, errors.New("report requires sequence 1..9007199254740991 and nonempty UTF-8 text of at most 4096 bytes")
	}
	for _, secret := range []string{r.request.Owner, r.owner, r.endpoint.Connection().Token} {
		if strings.Contains(q.Text, secret) {
			return BridgeReportResult{}, errors.New("report text contains a bridge capability; remove secrets before reporting")
		}
	}
	digest := sha256.Sum256([]byte(q.Kind + "\x00" + q.Text))
	r.op.Lock()
	defer r.op.Unlock()
	if err := ctx.Err(); err != nil {
		return BridgeReportResult{}, err
	}
	result := BridgeReportResult{BridgeID: r.id, Sequence: q.Sequence}
	if q.Sequence == r.reportSequence && digest == r.reportDigest {
		return result, nil
	}
	if q.Sequence <= r.reportSequence {
		return BridgeReportResult{}, errors.New("report sequence must increase; only the latest identical report may be retried")
	}
	s := r.snapshot()
	if bridgeEnded(s.State) || s.State == "recovering" || !time.Now().Before(s.ExpiresAt) || r.terminal.State() != session.StateOpen {
		return BridgeReportResult{}, errors.New("bridge is not accepting reports; debugging and recovery are managed separately")
	}
	r.terminal.PublishEvent(session.Event{
		Type: session.EventDebugClientReport, Actor: string(session.ActorAgent),
		Metadata: map[string]any{"bridge_id": r.id, "sequence": q.Sequence, "kind": q.Kind,
			"text": q.Text, "reported_by": "codex", "complete_audit": false},
	})
	r.reportSequence, r.reportDigest = q.Sequence, digest
	return result, nil
}
