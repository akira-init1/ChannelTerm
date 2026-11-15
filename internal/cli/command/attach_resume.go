package command

import (
	"context"
	"errors"
	"io"
)

// shortcutCheckpointTarget is optional local adapter metadata, not a generic
// Session capability. It rechecks the captured Host registration before resume.
type shortcutCheckpointTarget interface {
	fileTransferTarget(context.Context) (string, mcpListedSession, error)
}

func (s *mcpAttachSession) fileTransferTarget(ctx context.Context) (string, mcpListedSession, error) {
	if s.endpoint == "" || s.fileTargetID == "" {
		return "", mcpListedSession{}, errors.New("checkpoint needs an explicitly attached Host Session")
	}
	var listed struct {
		Sessions []mcpListedSession `json:"sessions"`
	}
	if err := s.call(ctx, "terminal_list_sessions", map[string]any{}, &listed); err != nil {
		return "", mcpListedSession{}, err
	}
	for _, candidate := range listed.Sessions {
		if candidate.ID == s.fileTargetID && candidate.Reference == s.recoveryReference && candidate.State == "open" {
			return s.endpoint, candidate, nil
		}
	}
	return "", mcpListedSession{}, ErrAttachedSessionNotFound
}

func (s nonClosingAttachSession) fileTransferTarget(ctx context.Context) (string, mcpListedSession, error) {
	if source, ok := s.attachSession.(shortcutCheckpointTarget); ok {
		return source.fileTransferTarget(ctx)
	}
	return "", mcpListedSession{}, errors.New("checkpoint needs a shared serial Host Session; use channelterm attach first")
}

func (s nonClosingAttachSession) keepFileCheckpoint() bool { return s.keepCheckpoint }

// runAttachCheckpoint uses the same file command and Core checkpoint validation
// as explicit CLI resume. Close only releases the temporary non-owning wrapper.
func runAttachCheckpoint(ctx context.Context, attached attachSession, output io.Writer, cancelRequested func() bool, direction, first, second string) error {
	source, ok := attached.(shortcutCheckpointTarget)
	if !ok {
		return errors.New("checkpoint target metadata is unavailable")
	}
	endpoint, selected, err := source.fileTransferTarget(ctx)
	if err != nil {
		return err
	}
	if selected.Transport != "serial" {
		return errors.New("checkpoints require serial; use the independent channelterm sftp command with --resume for a single SFTP file")
	}
	dependencies := fileCommandDependencies{
		newAttach: func(context.Context, string, string) (attachSession, error) { return attached, nil },
		listSessions: func(ctx context.Context, requested string) ([]mcpListedSession, error) {
			currentEndpoint, current, err := source.fileTransferTarget(ctx)
			if err != nil {
				return nil, err
			}
			if requested != currentEndpoint || currentEndpoint != endpoint || current.ID != selected.ID || current.Endpoint != selected.Endpoint {
				return nil, errors.New("checkpoint target changed; reselect the live Session")
			}
			return []mcpListedSession{current}, nil
		},
		transferCancelRequested: cancelRequested,
	}
	target := []string{"--session", selected.ID, "--endpoint", endpoint}
	switch direction {
	case "jobs":
		return runFileJobs(ctx, target, output, dependencies)
	case "resume":
		return runFileResume(ctx, append([]string{first}, target...), output, dependencies)
	}
	mode := "tar"
	if chooser, ok := attached.(interface{ fileTransferDirectoryMode() string }); ok {
		mode = chooser.fileTransferDirectoryMode()
	}
	if mode == "files" {
		return errors.New("retained checkpoints use TAR snapshots for directories; per-file mode cannot be combined with checkpoint retention")
	}
	args := append([]string{first, second}, target...)
	args = append(args, "--resumable", "--directory-mode", "tar")
	switch direction {
	case "send":
		return runFileSend(ctx, args, output, dependencies)
	case "receive":
		return runFileReceive(ctx, args, output, dependencies)
	default:
		return errors.New("unsupported checkpoint operation")
	}
}
