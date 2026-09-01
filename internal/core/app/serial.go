// Package app coordinates ChannelTerm application use cases.
//
// It composes configuration, transports, and Session ownership without adding
// protocol behavior to the Session Core or presentation behavior to adapters.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
)

const (
	serialTransportName = "serial"
	wakeByte            = 0x0D
	maxOpenAttempts     = 4
)

var (
	// ErrNilSessionManager is returned when a SerialService has no Session owner.
	ErrNilSessionManager = errors.New("session manager must not be nil")
	// ErrSessionIDExhausted is returned only when generated Session IDs collide repeatedly.
	ErrSessionIDExhausted = errors.New("could not allocate a unique session ID")
)

// ConnectedSession is a Session candidate that can establish its transport.
//
// SerialService transfers a successfully connected instance to its Manager.
// A failed candidate remains owned by SerialService and is closed before OpenSerial returns.
type ConnectedSession interface {
	session.Session
	Connect(context.Context) error
}

// SerialSessionFactory creates one unconnected serial Session for an ID.
type SerialSessionFactory func(string, serialtransport.Config) (ConnectedSession, error)

// SerialDependencies supplies replaceable boundaries for SerialService tests
// and for presentation adapters that need an in-memory terminal implementation.
// Nil values use the normal production implementation.
type SerialDependencies struct {
	ConfigPath   configPathResolver
	LoadConfig   configLoader
	SaveConfig   configSaver
	NewSession   SerialSessionFactory
	NewSessionID sessionIDGenerator
}

type configPathResolver func() (string, error)
type configLoader func(string) (config.File, error)
type configSaver func(string, config.File) error
type sessionIDGenerator func() (string, error)

// SerialService owns serial connection use cases over one shared Session Manager.
//
// The service resolves a profile, creates the Serial Transport and Session,
// connects it, and registers fixed Session metadata as one operation. It does
// not own the Manager itself; the embedding application closes that owner at
// process shutdown.
type SerialService struct {
	manager      *session.Manager
	configPath   configPathResolver
	loadConfig   configLoader
	saveConfig   configSaver
	newSession   SerialSessionFactory
	newSessionID sessionIDGenerator
}

// OpenSerialRequest describes a caller's serial connection intent.
//
// Overrides must contain only explicitly supplied values, preserving profile
// inheritance. Label is display metadata and is never used as an identifier.
type OpenSerialRequest struct {
	Profile    string
	ConfigPath string
	Overrides  config.SerialOverrides
	Save       string
	Label      string
}

// OpenSerialResult reports the Manager-owned Session created or reused by OpenSerial.
type OpenSerialResult struct {
	Info    session.SessionInfo
	Profile config.SerialProfile
	Reused  bool
}

// NewSerialService creates the normal production serial application service.
func NewSerialService(manager *session.Manager) (*SerialService, error) {
	return NewSerialServiceWithDependencies(manager, SerialDependencies{})
}

// NewSerialServiceWithDependencies creates a serial application service with
// explicitly replaceable construction dependencies.
//
// It is intended for adapters and unit tests that must avoid opening physical
// serial hardware. Production callers should use NewSerialService.
func NewSerialServiceWithDependencies(manager *session.Manager, dependencies SerialDependencies) (*SerialService, error) {
	if manager == nil {
		return nil, ErrNilSessionManager
	}
	service := &SerialService{
		manager:      manager,
		configPath:   config.DefaultPath,
		loadConfig:   config.LoadOrCreate,
		saveConfig:   config.Save,
		newSession:   newSerialSession,
		newSessionID: newSessionID,
	}
	if dependencies.ConfigPath != nil {
		service.configPath = dependencies.ConfigPath
	}
	if dependencies.LoadConfig != nil {
		service.loadConfig = dependencies.LoadConfig
	}
	if dependencies.SaveConfig != nil {
		service.saveConfig = dependencies.SaveConfig
	}
	if dependencies.NewSession != nil {
		service.newSession = dependencies.NewSession
	}
	if dependencies.NewSessionID != nil {
		service.newSessionID = dependencies.NewSessionID
	}
	return service, nil
}

// OpenSerial resolves configuration and opens or reuses one Manager-owned
// serial Session.
//
// ctx cancels profile opening, connection, and duplicate-open waiting. A wake
// carriage return is sent only when the final resolved profile explicitly
// enables it. A reused Session keeps the metadata and connection settings from
// its original open operation.
func (s *SerialService) OpenSerial(ctx context.Context, request OpenSerialRequest) (OpenSerialResult, error) {
	if err := ctx.Err(); err != nil {
		return OpenSerialResult{}, err
	}
	if s == nil || s.manager == nil {
		return OpenSerialResult{}, ErrNilSessionManager
	}
	path := strings.TrimSpace(request.ConfigPath)
	if path == "" {
		var err error
		path, err = s.configPath()
		if err != nil {
			return OpenSerialResult{}, fmt.Errorf("resolve serial configuration path: %w", err)
		}
	}
	file, err := s.loadConfig(path)
	if err != nil {
		return OpenSerialResult{}, fmt.Errorf("load serial configuration %q: %w", path, err)
	}
	profile, err := file.ResolveSerial(request.Profile)
	if err != nil {
		return OpenSerialResult{}, fmt.Errorf("resolve serial profile: %w", err)
	}
	profile = config.ApplySerialOverrides(profile, request.Overrides)
	if err := config.RequireSerialPort(profile); err != nil {
		return OpenSerialResult{}, err
	}
	if strings.TrimSpace(request.Save) != "" {
		file.SaveSerialProfile(request.Save, profile)
		if err := s.saveConfig(path, file); err != nil {
			return OpenSerialResult{}, err
		}
	}
	serialConfig := serialProfileToTransportConfig(profile)
	info, created, err := s.manager.GetOrCreate(ctx, session.SessionMetadata{
		Transport: serialTransportName,
		Endpoint:  serialConfig.Port,
		Label:     request.Label,
	}, func() (session.Session, error) {
		return s.createConnectedSerialSession(ctx, profile, serialConfig)
	})
	if err != nil {
		return OpenSerialResult{}, err
	}
	return OpenSerialResult{Info: info, Profile: profile, Reused: !created}, nil
}

// ListSessions returns the current Manager snapshot without exposing transports.
func (s *SerialService) ListSessions() []session.SessionInfo {
	if s == nil || s.manager == nil {
		return nil
	}
	return s.manager.ListInfo()
}

// GetSession returns a Manager-owned Session by opaque ID or short reference.
func (s *SerialService) GetSession(identifier string) (session.Session, bool) {
	if s == nil || s.manager == nil {
		return nil, false
	}
	return s.manager.Get(identifier)
}

// Reference returns the short Manager-assigned reference for identifier.
func (s *SerialService) Reference(identifier string) (string, bool) {
	if s == nil || s.manager == nil {
		return "", false
	}
	return s.manager.Reference(identifier)
}

// CloseSession removes and closes one Manager-owned Session.
//
// identifier can be an opaque Session ID or its Manager-assigned short
// reference. The false result means no such managed Session existed.
func (s *SerialService) CloseSession(identifier string) (bool, error) {
	if s == nil || s.manager == nil {
		return false, ErrNilSessionManager
	}
	terminal, ok := s.manager.Remove(identifier)
	if !ok {
		return false, nil
	}
	return true, terminal.Close()
}

// createConnectedSerialSession keeps failed candidates outside Manager ownership.
func (s *SerialService) createConnectedSerialSession(ctx context.Context, profile config.SerialProfile, serialConfig serialtransport.Config) (session.Session, error) {
	for range maxOpenAttempts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id, err := s.newSessionID()
		if err != nil {
			return nil, fmt.Errorf("generate session ID: %w", err)
		}
		if _, exists := s.manager.Get(id); exists {
			continue
		}
		terminal, err := s.newSession(id, serialConfig)
		if err != nil {
			return nil, fmt.Errorf("create serial session: %w", err)
		}
		if err := terminal.Connect(ctx); err != nil {
			return nil, closeCandidate(fmt.Errorf("connect serial port %q: %w", profile.Port, err), terminal)
		}
		if err := ctx.Err(); err != nil {
			return nil, closeCandidate(err, terminal)
		}
		if profile.Wake {
			if err := writeAll(ctx, terminal, session.WriteRequest{Actor: session.ActorSystem, Data: []byte{wakeByte}}); err != nil {
				return nil, closeCandidate(fmt.Errorf("wake serial session %q: %w", id, err), terminal)
			}
		}
		return terminal, nil
	}
	return nil, ErrSessionIDExhausted
}

// serialProfileToTransportConfig converts configuration storage values at the
// application boundary, leaving Transport independent from configuration files.
func serialProfileToTransportConfig(profile config.SerialProfile) serialtransport.Config {
	return serialtransport.Config{
		Port:        profile.Port,
		BaudRate:    profile.BaudRate,
		DataBits:    profile.DataBits,
		Parity:      serialtransport.Parity(profile.Parity),
		StopBits:    serialtransport.StopBits(profile.StopBits),
		FlowControl: serialtransport.FlowControl(profile.FlowControl),
	}
}

// newSerialSession constructs the normal Serial Transport and its Session Core.
func newSerialSession(id string, configuration serialtransport.Config) (ConnectedSession, error) {
	transport, err := serialtransport.New(configuration)
	if err != nil {
		return nil, err
	}
	return session.New(id, transport)
}

// newSessionID produces an opaque random identifier for Manager lookup.
func newSessionID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

// closeCandidate preserves an opening failure while releasing unregistered resources.
func closeCandidate(primary error, terminal ConnectedSession) error {
	if err := terminal.Close(); err != nil {
		return errors.Join(primary, fmt.Errorf("close incomplete serial session: %w", err))
	}
	return primary
}

// writeAll retries short writes so the opt-in wake byte follows Session's
// normal write semantics even with a Transport that accepts short writes.
func writeAll(ctx context.Context, terminal session.Session, request session.WriteRequest) error {
	for len(request.Data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := terminal.Write(request)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		request.Data = request.Data[n:]
	}
	return nil
}
