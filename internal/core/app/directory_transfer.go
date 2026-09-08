package app

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	// ErrDirectoryTarUnavailable is returned when the POSIX target has no tar
	// command. ChannelTerm does not install target-side software automatically.
	ErrDirectoryTarUnavailable = errors.New("Remote tar command is unavailable.\nDirectory transfer is not supported on this target.")
	// ErrUnsupportedDirectoryEntry is returned for local filesystem entries
	// that the first directory-transfer version deliberately does not preserve.
	ErrUnsupportedDirectoryEntry = errors.New("directory transfer supports only regular files and directories")
	// ErrUnsafeTarPath is returned when a remote tar member could escape the
	// caller-owned extraction directory.
	ErrUnsafeTarPath = errors.New("unsafe tar entry path")
)

// RemotePathKind identifies the supported kind of a target-side source.
type RemotePathKind string

const (
	// RemotePathFile is a regular remote file.
	RemotePathFile RemotePathKind = "file"
	// RemotePathDirectory is a remote directory.
	RemotePathDirectory RemotePathKind = "directory"
	// RemotePathMissing does not exist on the target.
	RemotePathMissing RemotePathKind = "missing"
	// RemotePathUnsupported is a symlink or other unsupported special entry.
	RemotePathUnsupported RemotePathKind = "unsupported"
)

// DirectoryTransferResult reports the tar stream byte count and selected
// remote destination for a directory transfer. Size counts tar stream bytes,
// not the sum of local file sizes.
type DirectoryTransferResult struct {
	Size       int64
	RemotePath string
}

type directoryEntry struct {
	name string
	path string
	head tar.Header
}

type directoryPlan struct {
	entries []directoryEntry
	size    int64
}

// PlanDirectory validates source and calculates the exact bytes emitted by
// Go's archive/tar writer without materializing an archive in memory or on
// disk. It is exported so callers can reject unsupported local sources before
// acquiring a shared Session lease.
func PlanDirectory(source string) (int64, error) {
	plan, err := makeDirectoryPlan(source)
	if err != nil {
		return 0, err
	}
	return plan.size, nil
}

func makeDirectoryPlan(source string) (directoryPlan, error) {
	root, err := filepath.Abs(source)
	if err != nil {
		return directoryPlan{}, fmt.Errorf("resolve local directory %q: %w", source, err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return directoryPlan{}, fmt.Errorf("stat local source %q: %w", source, err)
	}
	if !rootInfo.IsDir() {
		return directoryPlan{}, fmt.Errorf("local source %q is not a directory", source)
	}
	plan := directoryPlan{}
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return fmt.Errorf("%w: %q (%s)", ErrUnsupportedDirectoryEntry, current, info.Mode().Type())
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		head, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("make tar header for %q: %w", current, err)
		}
		head.Name = name
		if info.IsDir() {
			head.Typeflag = tar.TypeDir
			head.Size = 0
		} else {
			head.Typeflag = tar.TypeReg
		}
		plan.entries = append(plan.entries, directoryEntry{name: name, path: current, head: *head})
		return nil
	})
	if err != nil {
		return directoryPlan{}, fmt.Errorf("scan local directory %q: %w", source, err)
	}
	count := &countingWriter{}
	if err := plan.writeTo(count); err != nil {
		return directoryPlan{}, err
	}
	plan.size = count.size
	return plan, nil
}

func (p directoryPlan) writeTo(destination io.Writer) (err error) {
	writer := tar.NewWriter(destination)
	defer func() {
		if closeErr := writer.Close(); err == nil {
			err = closeErr
		}
	}()
	for _, entry := range p.entries {
		head := entry.head
		if err := writer.WriteHeader(&head); err != nil {
			return err
		}
		if entry.head.Typeflag != tar.TypeReg {
			continue
		}
		if _, ok := destination.(*countingWriter); ok {
			if _, err := io.CopyN(writer, zeroReader{}, entry.head.Size); err != nil {
				return err
			}
			continue
		}
		file, err := os.Open(entry.path)
		if err != nil {
			return fmt.Errorf("open local source %q: %w", entry.path, err)
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() != entry.head.Size {
			_ = file.Close()
			if statErr != nil {
				return fmt.Errorf("stat local source %q: %w", entry.path, statErr)
			}
			return fmt.Errorf("local source changed while sending: %q", entry.path)
		}
		_, copyErr := io.CopyN(writer, file, entry.head.Size)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("read local source %q: %w", entry.path, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close local source %q: %w", entry.path, closeErr)
		}
	}
	return nil
}

func (p directoryPlan) stream() io.Reader {
	reader, writer := io.Pipe()
	go func() {
		writer.CloseWithError(p.writeTo(writer))
	}()
	return reader
}

type countingWriter struct{ size int64 }

func (w *countingWriter) Write(data []byte) (int, error) {
	w.size += int64(len(data))
	return len(data), nil
}

type zeroReader struct{}

func (zeroReader) Read(data []byte) (int, error) {
	for index := range data {
		data[index] = 0
	}
	return len(data), nil
}

// SendDirectory creates a standard tar stream directly from source and asks
// the remote tar command to extract it into a same-directory staging path.
// The final rename happens only after tar has consumed the complete stream.
func SendDirectory(ctx context.Context, terminal FileTransferSession, source, remotePath string, progress FileTransferProgress) (DirectoryTransferResult, error) {
	if terminal == nil {
		return DirectoryTransferResult{}, errors.New("file transfer session must not be nil")
	}
	plan, err := makeDirectoryPlan(source)
	if err != nil {
		return DirectoryTransferResult{}, err
	}
	remotePath = strings.TrimSuffix(remotePath, "/")
	if remotePath == "" {
		return DirectoryTransferResult{}, errors.New("remote directory path must not be empty")
	}
	if _, err := quoteRemotePath(remotePath); err != nil {
		return DirectoryTransferResult{}, err
	}
	protocol, err := newFileProtocol(ctx, terminal)
	if err != nil {
		return DirectoryTransferResult{}, err
	}
	remotePath, quotedPath, err := protocol.selectAvailableRemotePath(ctx, remotePath)
	if err != nil {
		return DirectoryTransferResult{}, err
	}
	stagePath := posixDirectoryStagePath(remotePath, protocol.token)
	quotedStage, err := quoteRemotePath(stagePath)
	if err != nil {
		return DirectoryTransferResult{}, err
	}
	if err := protocol.command(ctx, directorySendInitCommand(protocol.token, quotedPath, quotedStage, plan.size)); err != nil {
		return DirectoryTransferResult{}, err
	}
	if _, err := protocol.expect(ctx, "READY", fmt.Sprintf("%d", plan.size)); err != nil {
		return DirectoryTransferResult{}, directoryTarError(err)
	}
	if sent, err := copyDirectoryStream(ctx, terminal, plan.stream(), plan.size, progress); err != nil {
		cleanupErr := protocol.finishDirectorySend(plan.size - sent)
		return DirectoryTransferResult{}, errors.Join(err, cleanupErr)
	}
	if _, err := protocol.expect(ctx, "FINAL", "OK"); err != nil {
		return DirectoryTransferResult{}, directoryTarError(err)
	}
	return DirectoryTransferResult{Size: plan.size, RemotePath: remotePath}, nil
}

// DetectRemotePath determines whether remotePath is a regular file, a
// directory, absent, or unsupported without following symlinks.
func DetectRemotePath(ctx context.Context, terminal FileTransferSession, remotePath string) (RemotePathKind, error) {
	if terminal == nil {
		return "", errors.New("file transfer session must not be nil")
	}
	quotedPath, err := quoteRemotePath(remotePath)
	if err != nil {
		return "", err
	}
	protocol, err := newFileProtocol(ctx, terminal)
	if err != nil {
		return "", err
	}
	if err := protocol.command(ctx, remotePathKindCommand(protocol.token, quotedPath)); err != nil {
		return "", err
	}
	event, err := protocol.expect(ctx, "KIND")
	if err != nil {
		return "", err
	}
	if len(event) != 1 {
		return "", fmt.Errorf("%w: KIND has %d arguments, want 1", ErrFileTransferProtocol, len(event))
	}
	kind := RemotePathKind(event[0])
	switch kind {
	case RemotePathFile, RemotePathDirectory, RemotePathMissing, RemotePathUnsupported:
		return kind, nil
	default:
		return "", fmt.Errorf("%w: remote path kind %q", ErrFileTransferProtocol, kind)
	}
}

// ReceiveDirectory streams a target-side tar archive into destination, which
// must be an empty staging directory owned by the caller. Go's tar extractor
// rejects paths and entry types outside the supported first-version contract.
func ReceiveDirectory(ctx context.Context, terminal FileTransferSession, remotePath, destination string, progress FileTransferProgress) (DirectoryTransferResult, error) {
	if terminal == nil {
		return DirectoryTransferResult{}, errors.New("file transfer session must not be nil")
	}
	quotedPath, err := quoteRemotePath(remotePath)
	if err != nil {
		return DirectoryTransferResult{}, err
	}
	if info, err := os.Stat(destination); err != nil || !info.IsDir() {
		if err != nil {
			return DirectoryTransferResult{}, fmt.Errorf("inspect extraction staging directory %q: %w", destination, err)
		}
		return DirectoryTransferResult{}, fmt.Errorf("extraction staging path %q is not a directory", destination)
	}
	protocol, err := newFileProtocol(ctx, terminal)
	if err != nil {
		return DirectoryTransferResult{}, err
	}
	if err := protocol.command(ctx, directoryReceiveSizeCommand(protocol.token, quotedPath)); err != nil {
		return DirectoryTransferResult{}, err
	}
	event, err := protocol.expectPhase(ctx, "SIZE")
	if err != nil {
		return DirectoryTransferResult{}, directoryTarError(err)
	}
	if len(event) != 1 {
		return DirectoryTransferResult{}, fmt.Errorf("%w: SIZE has %d arguments, want 1", ErrFileTransferProtocol, len(event))
	}
	size, err := parseNonNegativeSize(event[0])
	if err != nil {
		return DirectoryTransferResult{}, err
	}
	reader, writer := io.Pipe()
	extractDone := make(chan error, 1)
	go func() {
		err := extractDirectoryTar(reader, destination)
		_ = reader.CloseWithError(err)
		extractDone <- err
	}()
	if err := protocol.command(ctx, directoryReceiveStartCommand(protocol.token, quotedPath, size)); err != nil {
		_ = writer.CloseWithError(err)
		<-extractDone
		return DirectoryTransferResult{}, err
	}
	if _, err := protocol.expect(ctx, "DATA", fmt.Sprintf("%d", size)); err != nil {
		_ = writer.CloseWithError(err)
		<-extractDone
		return DirectoryTransferResult{}, directoryTarError(err)
	}
	copyErr := receiveDirectoryStream(ctx, protocol, writer, size, progress)
	_ = writer.CloseWithError(copyErr)
	extractErr := <-extractDone
	if copyErr != nil {
		return DirectoryTransferResult{}, copyErr
	}
	if _, err := protocol.expect(ctx, "ACK", fmt.Sprintf("%d", size)); err != nil {
		return DirectoryTransferResult{}, directoryTarError(err)
	}
	if extractErr != nil {
		return DirectoryTransferResult{}, extractErr
	}
	return DirectoryTransferResult{Size: size}, nil
}

func copyDirectoryStream(ctx context.Context, terminal FileTransferSession, source io.Reader, size int64, progress FileTransferProgress) (int64, error) {
	buffer := make([]byte, FileTransferChunkSize)
	var transferred int64
	for transferred < size {
		chunkSize := int64(len(buffer))
		if remaining := size - transferred; remaining < chunkSize {
			chunkSize = remaining
		}
		chunk := buffer[:int(chunkSize)]
		if _, err := io.ReadFull(source, chunk); err != nil {
			return transferred, fmt.Errorf("read local tar stream at byte %d: %w", transferred, err)
		}
		written, err := writeFilePayload(ctx, terminal, chunk)
		transferred += int64(written)
		if progress != nil && written > 0 {
			if progressErr := progress(transferred, size); progressErr != nil {
				return transferred, progressErr
			}
		}
		if err != nil {
			return transferred, fmt.Errorf("send tar stream at byte %d: %w", transferred-int64(written), err)
		}
	}
	var trailing [1]byte
	if count, readErr := source.Read(trailing[:]); count != 0 || (readErr != nil && !errors.Is(readErr, io.EOF)) {
		if readErr == nil {
			readErr = ErrFileTransferSizeMismatch
		}
		return transferred, fmt.Errorf("local directory changed while sending: %w", readErr)
	}
	if progress != nil && size == 0 {
		return transferred, progress(0, 0)
	}
	return transferred, nil
}

func receiveDirectoryStream(ctx context.Context, protocol *fileProtocol, destination io.Writer, size int64, progress FileTransferProgress) error {
	buffer := make([]byte, FileTransferChunkSize)
	var transferred int64
	var writeErr error
	for transferred < size {
		chunkSize := int64(len(buffer))
		if remaining := size - transferred; remaining < chunkSize {
			chunkSize = remaining
		}
		chunk := buffer[:int(chunkSize)]
		if _, err := protocol.readExact(ctx, chunk); err != nil {
			return fmt.Errorf("receive tar stream at byte %d: %w", transferred, err)
		}
		if writeErr == nil {
			writeErr = writeAllTo(destination, chunk)
		}
		transferred += chunkSize
		if progress != nil {
			if err := progress(transferred, size); err != nil && writeErr == nil {
				writeErr = err
			}
		}
	}
	return writeErr
}

func extractDirectoryTar(source io.Reader, destination string) error {
	root, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve extraction staging directory: %w", err)
	}
	reader := tar.NewReader(source)
	for {
		head, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar stream: %w", err)
		}
		target, err := safeTarTarget(root, head.Name)
		if err != nil {
			return err
		}
		switch head.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, head.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("create extracted directory %q: %w", target, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create parent for extracted file %q: %w", target, err)
			}
			file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, head.FileInfo().Mode().Perm())
			if err != nil {
				return fmt.Errorf("create extracted file %q: %w", target, err)
			}
			_, copyErr := io.Copy(file, reader)
			closeErr := file.Close()
			if copyErr != nil {
				return fmt.Errorf("extract file %q: %w", target, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close extracted file %q: %w", target, closeErr)
			}
		default:
			return fmt.Errorf("%w: %q has type %d", ErrUnsupportedDirectoryEntry, head.Name, head.Typeflag)
		}
	}
}

func safeTarTarget(root, name string) (string, error) {
	if name == "" || path.IsAbs(name) || filepath.IsAbs(name) || strings.HasPrefix(name, "\\") {
		return "", fmt.Errorf("%w: %q", ErrUnsafeTarPath, name)
	}
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' })
	for _, part := range parts {
		if part == ".." {
			return "", fmt.Errorf("%w: %q", ErrUnsafeTarPath, name)
		}
	}
	target := filepath.Join(root, filepath.FromSlash(name))
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrUnsafeTarPath, name)
	}
	return target, nil
}

func parseNonNegativeSize(value string) (int64, error) {
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil || size < 0 {
		return 0, fmt.Errorf("%w: invalid tar stream size %q", ErrFileTransferProtocol, value)
	}
	return size, nil
}

func posixDirectoryStagePath(destination, token string) string {
	directory, name := path.Split(destination)
	return path.Join(directory, "."+name+".cterm-part-"+token)
}

func directoryTarError(err error) error {
	if err != nil && strings.Contains(err.Error(), "remote file transfer failed: tar") {
		return ErrDirectoryTarUnavailable
	}
	return err
}
