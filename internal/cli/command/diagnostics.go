package command

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

// runDiagnostics downloads a Host archive into an explicitly selected local
// file. It never treats a Host path as a path on the calling machine.
func runDiagnostics(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("diagnostics", flag.ContinueOnError)
	flags.SetOutput(output)
	endpoint := flags.String("endpoint", defaultMCPEndpoint, "Session Host MCP endpoint")
	id := flags.String("session", "", "Session ID or short reference for a new snapshot")
	diagnosticID := flags.String("diagnostic-id", "", "download an existing archive instead of creating a snapshot")
	destination := flags.String("output", "", "new local ZIP path (never overwritten)")
	includeRaw := flags.Bool("include-raw", false, "explicitly include sensitive terminal contents in a new snapshot")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: channelterm diagnostics --session SESSION --output FILE.zip [--include-raw] [--endpoint URL]")
		fmt.Fprintln(output, "       channelterm diagnostics --diagnostic-id ID --output FILE.zip [--endpoint URL]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *destination == "" || (*id == "") == (*diagnosticID == "") || *diagnosticID != "" && *includeRaw {
		return errors.New("provide --output and exactly one of --session or --diagnostic-id; --include-raw requires --session")
	}
	remote, _, err := connectMCPClient(ctx, *endpoint)
	if err != nil {
		return err
	}
	defer remote.Close()
	var archive app.DiagnosticArchive
	if *id != "" {
		if err = callMCPTool(ctx, remote, "terminal_export_diagnostics", map[string]any{"session_id": *id, "include_raw": *includeRaw}, &archive); err != nil {
			return err
		}
	} else {
		archive.ID = *diagnosticID
		// Existing operation archives report metadata on every download chunk.
	}
	read := func(offset int) (diagnosticDownloadChunk, error) {
		var chunk diagnosticDownloadChunk
		err := callMCPTool(ctx, remote, "terminal_read_diagnostic", map[string]any{"diagnostic_id": archive.ID, "offset": offset, "max_bytes": 65536}, &chunk)
		return chunk, err
	}
	if err = saveDiagnostic(ctx, *destination, archive, read); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "Saved diagnostic archive %s to %s\n", archive.ID, *destination)
	return err
}

type diagnosticDownloadChunk struct {
	Data      string `json:"data"`
	Encoding  string `json:"encoding"`
	Next      int    `json:"next"`
	BytesRead int    `json:"bytes_read"`
	EOF       bool   `json:"eof"`
	Size      int    `json:"size"`
	SHA256    string `json:"sha256"`
}

// saveDiagnostic never overwrites an existing path. A failed download removes
// only the newly created file, and successful downloads verify size and digest.
func saveDiagnostic(ctx context.Context, path string, archive app.DiagnosticArchive, read func(int) (diagnosticDownloadChunk, error)) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		_ = f.Close()
		if !complete {
			_ = os.Remove(path)
		}
	}()
	hash := sha256.New()
	offset := 0
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		chunk, e := read(offset)
		if e != nil {
			return e
		}
		if archive.SHA256 == "" {
			archive.SHA256, archive.Size = chunk.SHA256, chunk.Size
		}
		if archive.Size < 0 || archive.Size > 16*1024*1024 || chunk.Size != archive.Size || chunk.SHA256 != archive.SHA256 {
			return errors.New("diagnostic archive metadata changed or is invalid")
		}
		data, e := base64.StdEncoding.DecodeString(chunk.Data)
		if e != nil || chunk.Encoding != "base64" || len(data) > 65536 || chunk.BytesRead != len(data) || chunk.Next != offset+len(data) || chunk.Next > archive.Size || !chunk.EOF && len(data) == 0 {
			return errors.New("invalid diagnostic download chunk")
		}
		if _, err = io.MultiWriter(f, hash).Write(data); err != nil {
			return err
		}
		offset = chunk.Next
		if chunk.EOF {
			if offset != archive.Size || hex.EncodeToString(hash.Sum(nil)) != archive.SHA256 {
				return errors.New("diagnostic archive checksum or size mismatch")
			}
			break
		}
	}
	if err = f.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}
