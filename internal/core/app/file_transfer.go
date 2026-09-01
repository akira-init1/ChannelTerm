package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

const (
	// FileTransferChunkSize bounds each in-memory payload and each raw-terminal
	// interval used by the simple Linux shell transfer protocol.
	FileTransferChunkSize = 32 * 1024
	fileProtocolReadSize  = 32 * 1024
	fileCleanupTimeout    = 5 * time.Second
)

var (
	// ErrFileTransferProtocol is returned when the remote shell emits an invalid
	// or unexpected ChannelTerm file-transfer control marker.
	ErrFileTransferProtocol = errors.New("invalid file transfer protocol response")
	// ErrFileTransferSizeMismatch is returned when the verified byte count does
	// not match the announced or locally supplied file size.
	ErrFileTransferSizeMismatch = errors.New("file transfer size mismatch")
	// ErrFileTransferChecksumMismatch is returned when SHA-256 verification
	// differs between the sending and receiving endpoints.
	ErrFileTransferChecksumMismatch = errors.New("file transfer SHA-256 mismatch")
)

// FileTransferSession is the cursor-based Session view required by file
// transfer. Implementations may be local Session adapters or remote CLI
// attachments, but must ultimately read and write through one Session.
type FileTransferSession interface {
	ReadRecent(context.Context, int) (session.OutputChunk, error)
	ReadOutput(context.Context, session.OutputCursor, int) (session.OutputChunk, error)
	Write(session.WriteRequest) (int, error)
}

// FileTransferProgress receives a confirmed byte count and the total size.
// Returning an error stops the transfer and propagates the presentation or I/O
// failure to the caller.
type FileTransferProgress func(transferred, total int64) error

// FileTransferResult reports the verified file metadata after transfer.
type FileTransferResult struct {
	Size   int64
	SHA256 string
}

// SendFile streams size bytes from source through terminal into remotePath.
//
// The remote endpoint must be an idle POSIX-compatible Linux shell providing
// stty, dd with iflag=fullblock, wc, and sha256sum. Each payload chunk is sent
// through Session.Write and acknowledged only after dd appends it. The final
// byte count and SHA-256 digest are independently computed by the remote shell.
func SendFile(ctx context.Context, terminal FileTransferSession, source io.Reader, size int64, remotePath string, progress FileTransferProgress) (FileTransferResult, error) {
	if terminal == nil {
		return FileTransferResult{}, errors.New("file transfer session must not be nil")
	}
	if source == nil {
		return FileTransferResult{}, errors.New("file transfer source must not be nil")
	}
	if size < 0 {
		return FileTransferResult{}, errors.New("file transfer size must not be negative")
	}
	quotedPath, err := quoteRemotePath(remotePath)
	if err != nil {
		return FileTransferResult{}, err
	}
	protocol, err := newFileProtocol(ctx, terminal)
	if err != nil {
		return FileTransferResult{}, err
	}
	if err := protocol.command(ctx, sendInitCommand(protocol.token, quotedPath)); err != nil {
		return FileTransferResult{}, err
	}
	if _, err := protocol.expect(ctx, "INIT", "OK"); err != nil {
		return FileTransferResult{}, fmt.Errorf("initialize remote file %q: %w", remotePath, err)
	}

	hasher := sha256.New()
	buffer := make([]byte, FileTransferChunkSize)
	var transferred int64
	for transferred < size {
		chunkSize := int64(len(buffer))
		if remaining := size - transferred; remaining < chunkSize {
			chunkSize = remaining
		}
		chunk := buffer[:int(chunkSize)]
		if _, err := io.ReadFull(source, chunk); err != nil {
			return FileTransferResult{}, fmt.Errorf("read local file at byte %d: %w", transferred, err)
		}
		if err := protocol.command(ctx, sendChunkCommand(protocol.token, quotedPath, chunkSize)); err != nil {
			return FileTransferResult{}, err
		}
		if _, err := protocol.expect(ctx, "READY", strconv.FormatInt(chunkSize, 10)); err != nil {
			return FileTransferResult{}, fmt.Errorf("prepare remote file chunk at byte %d: %w", transferred, err)
		}
		written, err := writeFilePayload(ctx, terminal, chunk)
		if err != nil {
			cleanupErr := protocol.finishSendChunk(chunkSize-int64(written), chunkSize)
			return FileTransferResult{}, errors.Join(fmt.Errorf("send file chunk at byte %d: %w", transferred, err), cleanupErr)
		}
		if _, err := protocol.expect(ctx, "ACK", strconv.FormatInt(chunkSize, 10)); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				_ = protocol.finishPendingMarker("ACK", strconv.FormatInt(chunkSize, 10))
			}
			return FileTransferResult{}, fmt.Errorf("confirm remote file chunk at byte %d: %w", transferred, err)
		}
		if _, err := hasher.Write(chunk); err != nil {
			return FileTransferResult{}, err
		}
		transferred += chunkSize
		if progress != nil {
			if err := progress(transferred, size); err != nil {
				return FileTransferResult{}, err
			}
		}
	}
	var trailing [1]byte
	if count, readErr := source.Read(trailing[:]); count != 0 || (readErr != nil && !errors.Is(readErr, io.EOF)) {
		if readErr == nil {
			readErr = ErrFileTransferSizeMismatch
		}
		return FileTransferResult{}, fmt.Errorf("local file changed while sending: %w", readErr)
	}

	localDigest := hex.EncodeToString(hasher.Sum(nil))
	if err := protocol.command(ctx, verifyCommand(protocol.token, quotedPath)); err != nil {
		return FileTransferResult{}, err
	}
	event, err := protocol.expectPhase(ctx, "FINAL")
	if err != nil {
		return FileTransferResult{}, fmt.Errorf("verify remote file %q: %w", remotePath, err)
	}
	remoteSize, remoteDigest, err := parseVerifiedFile(event)
	if err != nil {
		return FileTransferResult{}, err
	}
	if remoteSize != size {
		return FileTransferResult{}, fmt.Errorf("%w: local=%d remote=%d", ErrFileTransferSizeMismatch, size, remoteSize)
	}
	if !strings.EqualFold(remoteDigest, localDigest) {
		return FileTransferResult{}, fmt.Errorf("%w: local=%s remote=%s", ErrFileTransferChecksumMismatch, localDigest, remoteDigest)
	}
	if progress != nil && size == 0 {
		if err := progress(0, 0); err != nil {
			return FileTransferResult{}, err
		}
	}
	return FileTransferResult{Size: size, SHA256: localDigest}, nil
}

// ReceiveFile streams remotePath from an idle Linux shell into destination.
//
// Metadata is collected before streaming. Each dd invocation emits at most one
// fixed-size block while the remote TTY is raw, after which its saved settings
// are restored. destination is caller-owned and is not closed by ReceiveFile.
func ReceiveFile(ctx context.Context, terminal FileTransferSession, destination io.Writer, remotePath string, progress FileTransferProgress) (FileTransferResult, error) {
	if terminal == nil {
		return FileTransferResult{}, errors.New("file transfer session must not be nil")
	}
	if destination == nil {
		return FileTransferResult{}, errors.New("file transfer destination must not be nil")
	}
	quotedPath, err := quoteRemotePath(remotePath)
	if err != nil {
		return FileTransferResult{}, err
	}
	protocol, err := newFileProtocol(ctx, terminal)
	if err != nil {
		return FileTransferResult{}, err
	}
	if err := protocol.command(ctx, receiveInitCommand(protocol.token, quotedPath)); err != nil {
		return FileTransferResult{}, err
	}
	metadata, err := protocol.expectPhase(ctx, "META")
	if err != nil {
		return FileTransferResult{}, fmt.Errorf("read remote file metadata %q: %w", remotePath, err)
	}
	size, remoteDigest, err := parseVerifiedFile(metadata)
	if err != nil {
		return FileTransferResult{}, err
	}

	hasher := sha256.New()
	buffer := make([]byte, FileTransferChunkSize)
	var transferred int64
	var blockIndex int64
	for transferred < size {
		chunkSize := int64(len(buffer))
		if remaining := size - transferred; remaining < chunkSize {
			chunkSize = remaining
		}
		if err := protocol.command(ctx, receiveChunkCommand(protocol.token, quotedPath, blockIndex, chunkSize)); err != nil {
			return FileTransferResult{}, err
		}
		if _, err := protocol.expect(ctx, "DATA", strconv.FormatInt(chunkSize, 10)); err != nil {
			return FileTransferResult{}, fmt.Errorf("start remote file chunk at byte %d: %w", transferred, err)
		}
		chunk := buffer[:int(chunkSize)]
		read, err := protocol.readExact(ctx, chunk)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				_ = protocol.finishReceiveChunk(chunk[int(read):])
			}
			return FileTransferResult{}, fmt.Errorf("receive remote file chunk at byte %d: %w", transferred, err)
		}
		if _, err := protocol.expect(ctx, "ACK", strconv.FormatInt(chunkSize, 10)); err != nil {
			return FileTransferResult{}, fmt.Errorf("confirm received file chunk at byte %d: %w", transferred, err)
		}
		if err := writeAllTo(destination, chunk); err != nil {
			return FileTransferResult{}, fmt.Errorf("write local file at byte %d: %w", transferred, err)
		}
		if _, err := hasher.Write(chunk); err != nil {
			return FileTransferResult{}, err
		}
		transferred += chunkSize
		blockIndex++
		if progress != nil {
			if err := progress(transferred, size); err != nil {
				return FileTransferResult{}, err
			}
		}
	}
	if err := protocol.command(ctx, verifyCommand(protocol.token, quotedPath)); err != nil {
		return FileTransferResult{}, err
	}
	finalMetadata, err := protocol.expectPhase(ctx, "FINAL")
	if err != nil {
		return FileTransferResult{}, fmt.Errorf("recheck remote file %q: %w", remotePath, err)
	}
	finalSize, finalDigest, err := parseVerifiedFile(finalMetadata)
	if err != nil {
		return FileTransferResult{}, err
	}
	if finalSize != size {
		return FileTransferResult{}, fmt.Errorf("%w: announced=%d final=%d", ErrFileTransferSizeMismatch, size, finalSize)
	}
	if !strings.EqualFold(finalDigest, remoteDigest) {
		return FileTransferResult{}, fmt.Errorf("remote file changed during transfer: %w: announced=%s final=%s", ErrFileTransferChecksumMismatch, remoteDigest, finalDigest)
	}
	localDigest := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(remoteDigest, localDigest) {
		return FileTransferResult{}, fmt.Errorf("%w: remote=%s local=%s", ErrFileTransferChecksumMismatch, remoteDigest, localDigest)
	}
	if progress != nil && size == 0 {
		if err := progress(0, 0); err != nil {
			return FileTransferResult{}, err
		}
	}
	return FileTransferResult{Size: size, SHA256: localDigest}, nil
}

// fileProtocol owns one independent Session output cursor and preserves bytes
// read beyond a control marker. That pending buffer is essential for receive:
// one Channel read can contain the DATA marker, raw payload, and ACK together.
type fileProtocol struct {
	terminal FileTransferSession
	token    string
	cursor   session.OutputCursor
	pending  []byte
}

// newFileProtocol starts at the current output tail so old prompts and marker-
// shaped terminal history cannot be mistaken for this transfer's responses.
func newFileProtocol(ctx context.Context, terminal FileTransferSession) (*fileProtocol, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var tokenBytes [12]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return nil, fmt.Errorf("generate file transfer token: %w", err)
	}
	recent, err := terminal.ReadRecent(ctx, 1)
	if err != nil {
		return nil, fmt.Errorf("initialize file transfer output cursor: %w", err)
	}
	return &fileProtocol{terminal: terminal, token: hex.EncodeToString(tokenBytes[:]), cursor: recent.Next}, nil
}

// command submits one shell line through Session instead of accessing Channel
// or Transport. Shell commands construct markers at runtime so input echo never
// contains the complete tokenized marker that expectPhase searches for.
func (p *fileProtocol) command(ctx context.Context, command string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := writeFilePayload(ctx, p.terminal, []byte(command+"\n"))
	if err != nil {
		return fmt.Errorf("write remote file transfer command: %w", err)
	}
	return nil
}

func (p *fileProtocol) expect(ctx context.Context, phase string, arguments ...string) ([]string, error) {
	event, err := p.expectPhase(ctx, phase)
	if err != nil {
		return nil, err
	}
	if len(event) != len(arguments) {
		return nil, fmt.Errorf("%w: %s has %d arguments, want %d", ErrFileTransferProtocol, phase, len(event), len(arguments))
	}
	for index := range arguments {
		if event[index] != arguments[index] {
			return nil, fmt.Errorf("%w: %s argument %d is %q, want %q", ErrFileTransferProtocol, phase, index, event[index], arguments[index])
		}
	}
	return event, nil
}

// expectPhase scans raw Session output for this transfer's random-token marker.
// Unrelated shell output is discarded only from this private cursor; it remains
// available to every other Session reader.
func (p *fileProtocol) expectPhase(ctx context.Context, phase string) ([]string, error) {
	prefix := []byte("@CTERM:" + p.token + ":")
	for {
		if start := bytes.Index(p.pending, prefix); start >= 0 {
			if end := bytes.IndexByte(p.pending[start:], '\n'); end >= 0 {
				end += start
				line := strings.TrimSuffix(string(p.pending[start:end]), "\r")
				p.pending = append(p.pending[:0], p.pending[end+1:]...)
				fields := strings.Split(line[len(prefix):], ":")
				if len(fields) == 0 {
					continue
				}
				if fields[0] == "ERROR" {
					return nil, fmt.Errorf("remote file transfer failed: %s", strings.Join(fields[1:], ":"))
				}
				if fields[0] != phase {
					return nil, fmt.Errorf("%w: received %s while waiting for %s", ErrFileTransferProtocol, fields[0], phase)
				}
				return fields[1:], nil
			}
			if start > 0 {
				p.pending = append(p.pending[:0], p.pending[start:]...)
			}
		} else if len(p.pending) > len(prefix)-1 {
			p.pending = append(p.pending[:0], p.pending[len(p.pending)-(len(prefix)-1):]...)
		}
		chunk, err := p.terminal.ReadOutput(ctx, p.cursor, fileProtocolReadSize)
		if err != nil {
			return nil, err
		}
		if chunk.Dropped {
			return nil, fmt.Errorf("%w: Session output was overwritten during transfer", ErrFileTransferProtocol)
		}
		p.cursor = chunk.Next
		p.pending = append(p.pending, chunk.Data...)
	}
}

// readExact consumes exactly one announced raw payload, retaining any following
// ACK bytes for expectPhase. It never requests or allocates the whole file.
func (p *fileProtocol) readExact(ctx context.Context, destination []byte) (int, error) {
	read := 0
	for read < len(destination) {
		if len(p.pending) > 0 {
			count := copy(destination[read:], p.pending)
			p.pending = append(p.pending[:0], p.pending[count:]...)
			read += count
			continue
		}
		chunk, err := p.terminal.ReadOutput(ctx, p.cursor, fileProtocolReadSize)
		if err != nil {
			return read, err
		}
		if chunk.Dropped {
			return read, fmt.Errorf("%w: Session output was overwritten during transfer", ErrFileTransferProtocol)
		}
		p.cursor = chunk.Next
		p.pending = append(p.pending, chunk.Data...)
	}
	return read, nil
}

// finishSendChunk pads only an already-started raw dd block after cancellation
// or a short write. Completing that bounded block lets the shell restore its
// saved TTY mode; the caller still receives the original transfer failure.
func (p *fileProtocol) finishSendChunk(remaining, acknowledgedSize int64) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), fileCleanupTimeout)
	defer cancel()
	if remaining > 0 {
		padding := make([]byte, remaining)
		if _, err := writeFilePayload(cleanupCtx, p.terminal, padding); err != nil {
			return fmt.Errorf("restore remote TTY after interrupted send: %w", err)
		}
	}
	return p.finishPendingMarker("ACK", strconv.FormatInt(acknowledgedSize, 10))
}

// finishReceiveChunk drains an already-started bounded dd output so the remote
// shell can finish the command and restore its saved TTY mode.
func (p *fileProtocol) finishReceiveChunk(remaining []byte) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), fileCleanupTimeout)
	defer cancel()
	if _, err := p.readExact(cleanupCtx, remaining); err != nil {
		return fmt.Errorf("drain interrupted receive chunk: %w", err)
	}
	return nil
}

func (p *fileProtocol) finishPendingMarker(phase, argument string) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), fileCleanupTimeout)
	defer cancel()
	_, err := p.expect(cleanupCtx, phase, argument)
	return err
}

func writeFilePayload(ctx context.Context, terminal FileTransferSession, data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		request := session.WriteRequest{Actor: session.ActorUser, Data: data}
		var count int
		var err error
		if contextual, ok := terminal.(interface {
			WriteContext(context.Context, session.WriteRequest) (int, error)
		}); ok {
			count, err = contextual.WriteContext(ctx, request)
		} else {
			count, err = terminal.Write(request)
		}
		if count > len(data) {
			count = len(data)
		}
		written += count
		data = data[count:]
		if err != nil {
			return written, err
		}
		if count == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func writeAllTo(destination io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := destination.Write(data)
		if written > len(data) {
			written = len(data)
		}
		data = data[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func quoteRemotePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("remote file path must not be empty")
	}
	if strings.ContainsAny(path, "\x00\r\n") {
		return "", errors.New("remote file path must not contain NUL, carriage return, or newline")
	}
	return "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'", nil
}

func parseVerifiedFile(fields []string) (int64, string, error) {
	if len(fields) != 2 {
		return 0, "", fmt.Errorf("%w: file metadata has %d fields, want 2", ErrFileTransferProtocol, len(fields))
	}
	size, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 10, 64)
	if err != nil || size < 0 {
		return 0, "", fmt.Errorf("%w: invalid file size %q", ErrFileTransferProtocol, fields[0])
	}
	digest := strings.ToLower(strings.TrimSpace(fields[1]))
	if len(digest) != sha256.Size*2 {
		return 0, "", fmt.Errorf("%w: invalid SHA-256 %q", ErrFileTransferProtocol, fields[1])
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return 0, "", fmt.Errorf("%w: invalid SHA-256 %q", ErrFileTransferProtocol, fields[1])
	}
	return size, digest, nil
}

func sendInitCommand(token, path string) string {
	return fmt.Sprintf("t='%s'; if command -v stty >/dev/null 2>&1 && command -v dd >/dev/null 2>&1 && command -v wc >/dev/null 2>&1 && command -v sha256sum >/dev/null 2>&1 && printf x | dd of=/dev/null bs=1 count=1 iflag=fullblock 2>/dev/null; then if : > %s; then printf '\\n@CTERM:%%s:INIT:OK\\n' \"$t\"; else printf '\\n@CTERM:%%s:ERROR:open\\n' \"$t\"; fi; else printf '\\n@CTERM:%%s:ERROR:tools\\n' \"$t\"; fi", token, path)
}

func sendChunkCommand(token, path string, size int64) string {
	return fmt.Sprintf("t='%s'; n=%d; if exec 3>> %s; then saved=$(stty -g 2>/dev/null); if [ -n \"$saved\" ] && stty raw -echo; then printf '\\n@CTERM:%%s:READY:%%s\\n' \"$t\" \"$n\"; if dd bs=\"$n\" count=1 iflag=fullblock 2>/dev/null >&3; then exec 3>&-; stty \"$saved\"; printf '\\n@CTERM:%%s:ACK:%%s\\n' \"$t\" \"$n\"; else exec 3>&-; stty \"$saved\"; printf '\\n@CTERM:%%s:ERROR:write\\n' \"$t\"; fi; else exec 3>&-; printf '\\n@CTERM:%%s:ERROR:tty\\n' \"$t\"; fi; else printf '\\n@CTERM:%%s:ERROR:open\\n' \"$t\"; fi", token, size, path)
}

func verifyCommand(token, path string) string {
	return fmt.Sprintf("t='%s'; if size=$(wc -c < %s 2>/dev/null) && digest=$(sha256sum < %s 2>/dev/null); then set -- $digest; printf '\\n@CTERM:%%s:FINAL:%%s:%%s\\n' \"$t\" \"$size\" \"$1\"; else printf '\\n@CTERM:%%s:ERROR:verify\\n' \"$t\"; fi", token, path, path)
}

func receiveInitCommand(token, path string) string {
	return fmt.Sprintf("t='%s'; if command -v stty >/dev/null 2>&1 && command -v dd >/dev/null 2>&1 && command -v wc >/dev/null 2>&1 && command -v sha256sum >/dev/null 2>&1; then if size=$(wc -c < %s 2>/dev/null) && digest=$(sha256sum < %s 2>/dev/null); then set -- $digest; printf '\\n@CTERM:%%s:META:%%s:%%s\\n' \"$t\" \"$size\" \"$1\"; else printf '\\n@CTERM:%%s:ERROR:read\\n' \"$t\"; fi; else printf '\\n@CTERM:%%s:ERROR:tools\\n' \"$t\"; fi", token, path, path)
}

func receiveChunkCommand(token, path string, blockIndex, size int64) string {
	return fmt.Sprintf("t='%s'; i=%d; n=%d; if exec 3< %s; then saved=$(stty -g 2>/dev/null); if [ -n \"$saved\" ] && stty raw -echo; then printf '\\n@CTERM:%%s:DATA:%%s\\n' \"$t\" \"$n\"; if dd bs=%d skip=\"$i\" count=1 2>/dev/null <&3; then exec 3<&-; stty \"$saved\"; printf '\\n@CTERM:%%s:ACK:%%s\\n' \"$t\" \"$n\"; else exec 3<&-; stty \"$saved\"; printf '\\n@CTERM:%%s:ERROR:read\\n' \"$t\"; fi; else exec 3<&-; printf '\\n@CTERM:%%s:ERROR:tty\\n' \"$t\"; fi; else printf '\\n@CTERM:%%s:ERROR:open\\n' \"$t\"; fi", token, blockIndex, size, path, FileTransferChunkSize)
}
