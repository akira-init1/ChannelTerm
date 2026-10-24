package command

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

// fileMachineWriter replaces only local presentation. Host events and transfer bytes stay unchanged.
type fileMachineWriter struct{ output io.Writer }

func (w *fileMachineWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *fileMachineWriter) emit(value any) error        { return json.NewEncoder(w.output).Encode(value) }

func machineFileOutput(output io.Writer, enabled bool) io.Writer {
	if enabled {
		return &fileMachineWriter{output: output}
	}
	return output
}

func runFileBrowse(ctx context.Context, args []string, output io.Writer, dependencies fileCommandDependencies) error {
	path := "."
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		path, args = args[0], args[1:]
	}
	options, err := parseFileOptions("file browse", args, output, writeFileUsage)
	if err != nil || options.help {
		return err
	}
	attached, id, err := attachFileSession(ctx, options, io.Discard, dependencies)
	if err != nil {
		return err
	}
	defer attached.Close()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var result app.SerialDirectory
	err = withFileTransferLease(ctx, attached, id, func(ctx context.Context) error {
		var listErr error
		result, listErr = app.ListSerialDirectory(ctx, internalFileTransferSession{attachSession: attached}, path)
		return listErr
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}
