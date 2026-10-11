package ssh

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
)

// SCPEntry is one validated relative path in a legacy SCP transfer. The first
// component is the requested source basename, never an arbitrary server path.
type SCPEntry struct {
	Path string
	Size int64
	Mode fs.FileMode
}

// ValidFileName accepts one portable path component, rejecting traversal,
// Windows drive/stream/device spellings, and protocol control characters.
func ValidFileName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:<>\"|?*\x00\r\n") || strings.TrimRight(name, " .") != name {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" {
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9' {
		return false
	}
	return true
}

func scpPath(value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("SCP path must be nonempty and contain no NUL or newline")
	}
	if !strings.HasPrefix(value, "/") {
		value = "./" + value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'", nil
}

func scpLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return "", fmt.Errorf("read SCP control record: %w", err)
	}
	return string(line[:len(line)-1]), nil
}

func scpAck(reader *bufio.Reader) error {
	status, err := reader.ReadByte()
	if err != nil {
		return err
	}
	if status == 0 {
		return nil
	}
	if status != 1 && status != 2 {
		return fmt.Errorf("invalid SCP acknowledgement %d", status)
	}
	message, err := scpLine(reader)
	if err != nil {
		return err
	}
	return fmt.Errorf("remote SCP error: %q", message)
}

func scpOK(writer io.Writer) error {
	n, err := writer.Write([]byte{0})
	if err == nil && n != 1 {
		return io.ErrShortWrite
	}
	return err
}

func (f *Files) runSCP(command string, transfer func(*bufio.Reader, io.Writer) error) (err error) {
	// Cancellation can interrupt channel opening or exec acknowledgement as
	// well as payload I/O. Preserve it at every return, not only after transfer.
	defer func() {
		if f.ctx.Err() != nil {
			err = errors.Join(f.ctx.Err(), err)
		}
	}()
	if f.sftp != nil {
		return errors.New("legacy SCP requires an SCP file connection")
	}
	if err := f.ctx.Err(); err != nil {
		return err
	}
	s, err := f.client.NewSession()
	if err != nil {
		return err
	}
	defer s.Close()
	input, err := s.StdinPipe()
	if err != nil {
		return err
	}
	output, err := s.StdoutPipe()
	if err != nil {
		return err
	}
	diagnostic := &scpDiagnostic{}
	s.Stderr = diagnostic
	if err := s.Start(command); err != nil {
		return fmt.Errorf("start remote scp: %w", err)
	}
	err = transfer(bufio.NewReaderSize(output, 4096), input)
	if err != nil {
		// A malformed peer must not retain copy workers or leave a reusable
		// connection whose protocol state is no longer known.
		_ = f.client.Close()
	} else {
		err = input.Close()
		// A download server may close its channel immediately after the final
		// acknowledgement. Closing our already-closed write half then returns
		// EOF even though the complete transfer and exit status succeeded.
		if errors.Is(err, io.EOF) {
			err = nil
		}
	}
	waitErr := s.Wait()
	// Wait joins the backend stderr copier before its bounded buffer is read.
	// Quoting prevents a remote diagnostic from injecting terminal controls.
	if (err != nil || waitErr != nil) && len(diagnostic.data) > 0 {
		err = errors.Join(err, fmt.Errorf("remote scp: %q", diagnostic.data))
	}
	return errors.Join(err, waitErr)
}

type scpDiagnostic struct{ data []byte }

func (d *scpDiagnostic) Write(data []byte) (int, error) {
	n := len(data)
	if remaining := 4096 - len(d.data); remaining > 0 {
		d.data = append(d.data, data[:min(remaining, len(data))]...)
	}
	return n, nil
}

// UploadSCP sends a regular file or directory tree using the legacy SCP wire
// protocol and a remote POSIX scp command. Names are literal, not shell globs.
// Source must be a confined filesystem; symlink/special entries are rejected.
// overwrite=false adds a remote existence guard, not an atomic reservation.
func (f *Files) UploadSCP(source fs.FS, name, destination string, recursive, overwrite bool, progress func(string, int64, int64) error) (files, totalBytes int64, err error) {
	command, err := scpUploadCommand(name, destination, recursive, overwrite)
	if err != nil {
		return 0, 0, err
	}
	return f.uploadSCP(source, name, recursive, progress, command)
}

// scpUploadCommand keeps the existence guard and the remote sink invocation
// together so both refer to the same destination chosen by the caller.
func scpUploadCommand(name, destination string, recursive, overwrite bool) (string, error) {
	if !ValidFileName(name) {
		return "", errors.New("invalid SCP source basename")
	}
	quoted, err := scpPath(destination)
	if err != nil {
		return "", err
	}
	command := "scp -t -- " + quoted
	if recursive {
		command = "scp -r -t -- " + quoted
	}
	if !overwrite {
		// Preserve dot components: resolving symlink/.. is a server filesystem
		// operation. Lexical cleaning here can inspect a different directory
		// than the literal path passed to scp and bypass overwrite protection.
		child, err := scpPath(strings.TrimRight(destination, "/") + "/" + name)
		if err != nil {
			return "", err
		}
		command = "if { test -d " + quoted + " && { test -e " + child + " || test -L " + child + "; }; } || { ! test -d " + quoted + " && { test -e " + quoted + " || test -L " + quoted + "; }; }; then printf '\\001destination exists\\n'; exit 1; fi; exec " + command
	}
	return command, nil
}

func (f *Files) uploadSCP(source fs.FS, name string, recursive bool, progress func(string, int64, int64) error, command string) (files, totalBytes int64, err error) {
	err = f.runSCP(command, func(reader *bufio.Reader, writer io.Writer) error {
		if err := scpAck(reader); err != nil {
			return err
		}
		var send func(string, int) error
		send = func(current string, depth int) error {
			if err := f.ctx.Err(); err != nil {
				return err
			}
			if depth > 128 {
				return errors.New("SCP directory depth exceeds 128")
			}
			info, err := fs.Stat(source, current)
			if err != nil {
				return err
			}
			base := path.Base(current)
			if !ValidFileName(base) {
				return errors.New("unsafe SCP filename")
			}
			if info.IsDir() {
				if !recursive {
					return errors.New("directory transfer requires --recursive")
				}
				if _, err := fmt.Fprintf(writer, "D%04o 0 %s\n", info.Mode().Perm(), base); err != nil {
					return err
				}
				if err := scpAck(reader); err != nil {
					return err
				}
				entries, err := fs.ReadDir(source, current)
				if err != nil {
					return err
				}
				for _, entry := range entries {
					if entry.Type()&fs.ModeSymlink != 0 || !ValidFileName(entry.Name()) {
						return errors.New("SCP does not follow symlinks or unsafe names")
					}
					if err := send(path.Join(current, entry.Name()), depth+1); err != nil {
						return err
					}
				}
				if _, err := io.WriteString(writer, "E\n"); err != nil {
					return err
				}
				return scpAck(reader)
			}
			if !info.Mode().IsRegular() {
				return errors.New("SCP supports only regular files and directories")
			}
			file, err := source.Open(current)
			if err != nil {
				return err
			}
			defer file.Close()
			if _, err := fmt.Fprintf(writer, "C%04o %d %s\n", info.Mode().Perm(), info.Size(), base); err != nil {
				return err
			}
			if err := scpAck(reader); err != nil {
				return err
			}
			counter := &scpProgressWriter{writer: writer, name: current, size: info.Size(), progress: progress}
			n, err := io.CopyN(counter, file, info.Size())
			totalBytes += n
			if err != nil {
				return err
			}
			if err := scpOK(writer); err != nil {
				return err
			}
			if err := scpAck(reader); err != nil {
				return err
			}
			files++
			return nil
		}
		return send(name, 0)
	})
	return files, totalBytes, err
}

// scpProgressWriter reports bytes accepted by the SSH stream, not durable
// remote storage. The protocol's final acknowledgement determines completion.
type scpProgressWriter struct {
	writer   io.Writer
	name     string
	size     int64
	written  int64
	progress func(string, int64, int64) error
}

func (w *scpProgressWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	w.written += int64(n)
	if err == nil && w.progress != nil {
		err = w.progress(w.name, w.written, w.size)
	}
	return n, err
}

// DownloadSCP delivers validated entries to receive synchronously. A file reader
// is limited to its declared size and must be fully consumed before returning.
// Directories have nil readers. The caller owns local destination policy and
// containment. Duplicate/traversal names, excessive nesting, and extra roots fail.
func (f *Files) DownloadSCP(source string, recursive bool, receive func(SCPEntry, io.Reader) error) error {
	quoted, err := scpPath(source)
	if err != nil {
		return err
	}
	expected := path.Base(path.Clean(source))
	if !ValidFileName(expected) {
		return errors.New("SCP source must name a file or directory")
	}
	command := "scp -f -- " + quoted
	if recursive {
		command = "scp -r -f -- " + quoted
	}
	return f.runSCP(command, func(reader *bufio.Reader, writer io.Writer) error {
		return receiveSCP(reader, writer, expected, recursive, receive)
	})
}

func receiveSCP(reader *bufio.Reader, writer io.Writer, expected string, recursive bool, receive func(SCPEntry, io.Reader) error) error {
	if err := scpOK(writer); err != nil {
		return err
	}
	var stack []string
	seen := make(map[string]bool)
	rootSeen := false
	for {
		kind, err := reader.ReadByte()
		if err == io.EOF && rootSeen && len(stack) == 0 {
			return nil
		}
		if err != nil {
			return fmt.Errorf("incomplete SCP transfer: %w", err)
		}
		line, err := scpLine(reader)
		if err != nil {
			return err
		}
		if kind == 1 || kind == 2 {
			return fmt.Errorf("remote SCP error: %q", line)
		}
		if kind == 'T' {
			parts := strings.Fields(line)
			if len(parts) != 4 {
				return errors.New("invalid SCP timestamp")
			}
			for _, part := range parts {
				if n, err := strconv.ParseInt(part, 10, 64); err != nil || n < 0 {
					return errors.New("invalid SCP timestamp")
				}
			}
			if err := scpOK(writer); err != nil {
				return err
			}
			continue
		}
		if kind == 'E' {
			if line != "" || len(stack) == 0 {
				return errors.New("unmatched SCP directory end")
			}
			stack = stack[:len(stack)-1]
			if err := scpOK(writer); err != nil {
				return err
			}
			continue
		}
		if kind != 'C' && kind != 'D' {
			return errors.New("unexpected SCP record")
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) != 3 || len(parts[0]) != 4 || !ValidFileName(parts[2]) {
			return errors.New("invalid SCP entry")
		}
		mode, modeErr := strconv.ParseUint(parts[0], 8, 12)
		size, sizeErr := strconv.ParseInt(parts[1], 10, 64)
		if modeErr != nil || sizeErr != nil || size < 0 || kind == 'D' && (size != 0 || !recursive) {
			return errors.New("invalid SCP size/mode or directory without --recursive")
		}
		if len(stack) == 0 {
			if rootSeen || parts[2] != expected {
				return errors.New("unexpected SCP root filename")
			}
			rootSeen = true
		}
		name := strings.Join(append(append([]string{}, stack...), parts[2]), "/")
		if seen[name] || len(seen) >= 100000 || len(stack) >= 128 {
			return errors.New("duplicate SCP entry or tree limit exceeded")
		}
		seen[name] = true
		entry := SCPEntry{Path: name, Size: size, Mode: fs.FileMode(mode) & 0777}
		if kind == 'D' {
			entry.Mode |= fs.ModeDir
			if err := receive(entry, nil); err != nil {
				return err
			}
			stack = append(stack, parts[2])
		} else {
			if err := scpOK(writer); err != nil {
				return err
			}
			body := &io.LimitedReader{R: reader, N: size}
			if err := receive(entry, body); err != nil {
				return err
			}
			if body.N != 0 {
				return io.ErrUnexpectedEOF
			}
			if err := scpAck(reader); err != nil {
				return err
			}
		}
		if err := scpOK(writer); err != nil {
			return err
		}
	}
}
