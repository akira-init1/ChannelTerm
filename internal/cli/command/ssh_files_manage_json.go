package command

import (
	"context"
	"errors"
	"io/fs"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

func isSSHManagementOperation(operation string) bool {
	switch operation {
	case "mkdir", "rename", "rm", "rmdir":
		return true
	default:
		return false
	}
}

// writeSSHManagementJSON reports the server acknowledgement only after the
// dedicated connection has closed successfully. Failure never proves absence of
// a mutation: connection loss or cancellation may follow a successful request.
// Only typed errors become safe reason codes; remote diagnostic text is omitted.
func writeSSHManagementJSON(ctx context.Context, encode func(any) error, r app.SSHFilesRequest, runErr error) error {
	err := errors.Join(runErr, ctx.Err())
	kind, outcome, code := "completed", "confirmed", ""
	if err != nil {
		kind, outcome, code = "failed", "unknown", sshFileFailureCode(err)
		if errors.Is(err, context.Canceled) {
			kind = "cancelled"
		}
	}
	return errors.Join(err, encode(map[string]any{
		"type": kind, "operation": r.Operation, "outcome": outcome, "error_code": code,
	}))
}

// sshFileFailureCode exposes only stable typed semantics, never remote diagnostics.
func sshFileFailureCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, app.ErrSSHFileVerificationMismatch):
		return "verification_failed"
	case errors.Is(err, fs.ErrPermission):
		return "permission_denied"
	case errors.Is(err, fs.ErrNotExist):
		return "not_found"
	case errors.Is(err, fs.ErrExist):
		return "already_exists"
	default:
		return "operation_failed"
	}
}
