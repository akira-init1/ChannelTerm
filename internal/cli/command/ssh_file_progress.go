package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

// sshFileProgress reuses serial transfer formatting without its Session events
// or lease semantics. Calls are serialized by the dedicated file operation.
// Payload progress is not a remote durability/commit acknowledgement: the final
// 100% frame is deferred until the next file starts or the operation succeeds.
type sshFileProgress struct {
	output              io.Writer
	now                 func() time.Time
	request             app.SSHFilesRequest
	name                string
	started, lastRender time.Time
	last                fileTransferSnapshot
	files               int64
	initial             int64
	active, rendered    bool
}

// Report starts each file, including empty files, and throttles payload frames
// to five per second. Names are quoted so remote control bytes stay inert.
func (p *sshFileProgress) Report(name string, transferred, total int64) error {
	now := p.now()
	if !p.active || name != p.name {
		if p.active {
			if p.last.transferred == p.last.total {
				p.last.percent = 100
				if err := p.render(); err != nil {
					return err
				}
			}
			if err := p.endLine(); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(p.output, "%s %s: Ctrl+C cancels.\n", strings.ToUpper(p.request.Protocol), p.request.Operation); err != nil {
				return err
			}
		}
		p.files++
		p.name, p.started, p.lastRender, p.active = name, now, time.Time{}, true
		p.initial = max(0, transferred)
		if _, err := fmt.Fprintf(p.output, "File %d: %q (%d completed)\n", p.files, name, p.files-1); err != nil {
			return err
		}
		p.last = fileTransferSnapshot{transferred: p.initial, total: max(0, total)}
		if total > 0 {
			p.last.percent = min(99.9, 100*float64(p.initial)/float64(total))
		}
		if err := p.render(); err != nil {
			return err
		}
		p.lastRender = now
	}
	transferred = max(0, transferred)
	total = max(0, total)
	percent := float64(0)
	if total > 0 {
		// Formatting rounds to one decimal; never round an unfinished file
		// to 100% before its close/acknowledgement/replacement succeeds.
		percent = min(99.9, 100*float64(transferred)/float64(total))
	}
	speed := float64(0)
	if elapsed := now.Sub(p.started).Seconds(); elapsed > 0 {
		speed = float64(max(0, transferred-p.initial)) / elapsed
	}
	p.last = fileTransferSnapshot{transferred: transferred, total: total, percent: percent, speed: speed}
	if transferred == total || now.Sub(p.lastRender) < 200*time.Millisecond {
		return nil
	}
	p.lastRender = now
	return p.render()
}

func (p *sshFileProgress) render() error {
	p.rendered = true
	return writeAll(p.output, []byte("\r"+formatFileTransferProgress(p.last)+"\x1b[K"))
}

func (p *sshFileProgress) endLine() error {
	if !p.rendered {
		return nil
	}
	p.rendered = false
	return writeAll(p.output, []byte("\n"))
}

// Finish ends the dynamic line before the command's diagnostic. Authoritative
// file counts come from Application, including partially completed directories.
func (p *sshFileProgress) Finish(result app.SSHFilesResult, operationErr error) error {
	if operationErr == nil && p.active {
		p.last.percent = 100
		if err := p.render(); err != nil {
			return err
		}
	}
	if err := p.endLine(); err != nil {
		return err
	}
	status := "completed"
	if operationErr != nil {
		status = "failed"
		if errors.Is(operationErr, context.Canceled) {
			status = "cancelled"
		}
	}
	if _, err := fmt.Fprintf(p.output, "%s %s %s: %d file(s) completed, %s transferred.\n", strings.ToUpper(p.request.Protocol), p.request.Operation, status, result.Files, formatFileTransferBytes(result.Bytes)); err != nil {
		return err
	}
	if operationErr == nil || !p.active {
		return nil
	}
	if p.request.SafeReplace {
		return writeAll(p.output, []byte("Safe replacement: incomplete copies do not replace the target. Completed files remain; remote temporary files may remain. Inspect the target if the final rename reply was lost.\n"))
	}
	return writeAll(p.output, []byte("Partial files may remain; --overwrite may have truncated an existing file. Inspect the destination before retrying.\n"))
}
