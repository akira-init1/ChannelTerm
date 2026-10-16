package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
)

// runRSPClient adapts GDB's pipe connection to the authenticated byte stream.
// It never invokes GDB. Connection secrets come from a private descriptor file,
// not argv. stdout contains only target bytes after successful authentication.
func runRSPClient(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("rsp-client", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("connection", "", "private JSON connection descriptor returned by debug_bridge_start")
	usage := func() {
		fmt.Fprintln(output, "Usage: channelterm rsp-client --connection FILE")
		fmt.Fprintln(output, "GDB pipe helper: target remote | channelterm rsp-client --connection FILE")
		fmt.Fprintln(output, "Authenticates pinned TLS/TCP and maintains a separate control heartbeat. Forwards RSP bytes unchanged; does not start or control GDB.")
		flags.SetOutput(output)
		flags.PrintDefaults()
		flags.SetOutput(io.Discard)
	}
	// Flag parsing errors must not write diagnostic text into GDB's byte pipe.
	flags.Usage = func() {}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage()
			return nil
		}
		return err
	}
	if *path == "" || flags.NArg() != 0 {
		return errors.New("rsp-client requires --connection FILE and no positional arguments")
	}
	file, err := os.Open(*path)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 4096 {
		return errors.New("connection descriptor must be a regular file of at most 4096 bytes")
	}
	if runtime.GOOS != "windows" && stat.Mode().Perm()&0077 != 0 {
		return errors.New("connection descriptor must be private; remove group/other permissions (chmod 600)")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	var info debugger.BridgeConnection
	if err = decoder.Decode(&info); err != nil {
		return fmt.Errorf("read bridge connection descriptor: %w", err)
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return errors.New("connection descriptor must contain exactly one JSON object")
	}
	conn, err := app.ConnectRSPBridge(ctx, info)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	done := make(chan error, 2)
	go func() { _, err := io.Copy(conn, input); done <- err }()
	go func() { _, err := io.Copy(output, conn); done <- err }()
	// stdin is caller-owned and can be blocked in a read. On remote EOF return
	// immediately; the standalone helper process exits and releases its stdin.
	// The buffered result channel ensures either copy can finish without blocking.
	err = <-done
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
