package guiapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

// SSHConfirmPath accepts only single-use decisions for a pending SSH handshake.
const SSHConfirmPath = "/gui/v1/ssh/confirm"

type sshTrustDecision struct {
	ctx    context.Context
	answer chan bool
}

// sshTrust binds unguessable decision capabilities to live handshake callbacks.
// It stores no credentials or key material. The SSH handler's two setup slots
// bound this map, and disconnect/deadline removes an unanswered capability.
type sshTrust struct {
	mu        sync.Mutex
	pending   map[string]sshTrustDecision
	decisions chan struct{}
}

func (s *sshTrust) confirm(ctx context.Context, w http.ResponseWriter, key sshtransport.HostKey) (bool, error) {
	capability := make([]byte, 32)
	if _, err := rand.Read(capability); err != nil {
		return false, err
	}
	id := hex.EncodeToString(capability)
	decision := sshTrustDecision{ctx: ctx, answer: make(chan bool, 1)}
	s.mu.Lock()
	s.pending[id] = decision
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, id); s.mu.Unlock() }()
	if err := json.NewEncoder(w).Encode(map[string]string{"event": "trust", "challenge_id": id, "address": key.Address, "algorithm": key.Algorithm, "fingerprint": key.Fingerprint}); err != nil {
		return false, err
	}
	if err := http.NewResponseController(w).Flush(); err != nil {
		return false, err
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case accept := <-decision.answer:
		return accept, nil
	}
}

func (s *sshTrust) serveDecision(w http.ResponseWriter, r *http.Request) {
	select {
	case s.decisions <- struct{}{}:
		defer func() { <-s.decisions }()
	default:
		reply(w, 429, map[string]string{"error": "busy"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		reply(w, 405, map[string]string{"error": "method_not_allowed"})
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		reply(w, 415, map[string]string{"error": "invalid_content_type"})
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer controller.SetReadDeadline(time.Time{})
	var input struct {
		ID     string `json:"challenge_id"`
		Accept *bool  `json:"accept"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || len(input.ID) != 64 || input.Accept == nil {
		reply(w, 400, map[string]string{"error": "invalid_input"})
		return
	}
	s.mu.Lock()
	decision, ok := s.pending[input.ID]
	if ok {
		delete(s.pending, input.ID)
	}
	s.mu.Unlock()
	if !ok || decision.ctx.Err() != nil {
		reply(w, 409, map[string]string{"error": "confirmation_expired"})
		return
	}
	select {
	case decision.answer <- *input.Accept:
		reply(w, 200, map[string]bool{"accepted": true})
	default:
		reply(w, 409, map[string]string{"error": "confirmation_expired"})
	}
}

// streamSSHResult keeps legacy JSON responses unchanged unless the caller opted
// into the advertised confirmation protocol. Each stream has at most one trust
// event and one terminal result/error; Transport still rejects changed/revoked keys.
func streamSSHResult(w http.ResponseWriter, stream bool, status int, value map[string]any) {
	if !stream {
		reply(w, status, value)
		return
	}
	if status == http.StatusOK {
		value["event"] = "result"
	} else {
		value["event"] = "error"
	}
	_ = json.NewEncoder(w).Encode(value)
}

var errDuplicateSSHTrust = errors.New("SSH trust confirmation already requested")
