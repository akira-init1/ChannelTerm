package command

import (
	"context"
	"fmt"
	"io"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

func (p *fileTransferReporter) directoryFileProgress(cancelRequested func() bool) app.DirectoryFileProgress {
	lastName := ""
	lastVerified := -1
	return func(name string, verified, totalFiles int, transferred, total int64) error {
		if cancelRequested != nil && cancelRequested() {
			return context.Canceled
		}
		p.base["current_file"] = name
		p.base["files_verified"] = verified
		p.base["files_total"] = totalFiles
		if machine, ok := p.output.(*fileMachineWriter); ok {
			if err := machine.emit(map[string]any{"type": "file_progress", "path": name, "files_verified": verified, "files_total": totalFiles, "transferred": transferred, "total": total}); err != nil {
				return err
			}
		} else if name != lastName || verified != lastVerified {
			if _, err := fmt.Fprintf(p.output, "\r\n[ChannelTerm] File: %q | verified %d/%d\r\n", name, verified, totalFiles); err != nil {
				return err
			}
		}
		lastName, lastVerified = name, verified
		return p.Report(transferred, total)
	}
}

func writeDirectoryFilesVerificationSummary(output io.Writer, digest, savedPath string, files int) error {
	if machine, ok := output.(*fileMachineWriter); ok {
		return machine.emit(map[string]any{"type": "verified", "sha256": digest, "checksum_scope": "manifest", "files_verified": files, "resolved_path": savedPath})
	}
	summary := fmt.Sprintf("  Files verified: %d (per-file SHA-256 readback)\n  Manifest SHA-256: %s\n  Saved         : %s\n", files, digest, savedPath)
	if deferred, ok := output.(interface{ deferFileTransferVerificationSummary(string) bool }); ok && deferred.deferFileTransferVerificationSummary(summary) {
		return nil
	}
	_, err := fmt.Fprint(output, summary)
	return err
}
