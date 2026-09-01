package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// TestSendFileStreamsSmallFileAndVerifiesMetadata covers binary payloads,
// acknowledged progress, size verification, and end-to-end hashing.
func TestSendFileStreamsSmallFileAndVerifiesMetadata(t *testing.T) {
	content := []byte("ChannelTerm\x00file\ntransfer\xff")
	terminal := newFileTransferTestSession(nil)
	var progress []int64
	result, err := SendFile(context.Background(), terminal, bytes.NewReader(content), int64(len(content)), "/tmp/firmware.bin", func(transferred, total int64) error {
		if total != int64(len(content)) {
			t.Errorf("progress total = %d, want %d", total, len(content))
		}
		progress = append(progress, transferred)
		return nil
	})
	if err != nil {
		t.Fatalf("SendFile() error = %v", err)
	}
	if !bytes.Equal(terminal.received, content) {
		t.Errorf("remote content = %x, want %x", terminal.received, content)
	}
	wantDigest := sha256.Sum256(content)
	if result.Size != int64(len(content)) || result.SHA256 != hex.EncodeToString(wantDigest[:]) {
		t.Errorf("SendFile() result = %+v, want size %d digest %x", result, len(content), wantDigest)
	}
	if len(progress) != 1 || progress[0] != int64(len(content)) {
		t.Errorf("progress = %v, want final size", progress)
	}
}

// TestSendFileStreamsLargeInputInBoundedChunks proves a 10 MiB source never
// becomes one source read or Session write.
func TestSendFileStreamsLargeInputInBoundedChunks(t *testing.T) {
	const size = 10*1024*1024 + 137
	source := &generatedFileReader{remaining: size}
	terminal := newFileTransferTestSession(nil)
	result, err := SendFile(context.Background(), terminal, source, size, "/tmp/large.bin", nil)
	if err != nil {
		t.Fatalf("SendFile() error = %v", err)
	}
	if result.Size != size || len(terminal.received) != size {
		t.Errorf("transferred size = %d/%d, want %d", result.Size, len(terminal.received), size)
	}
	if source.maxRead > FileTransferChunkSize {
		t.Errorf("largest source read = %d, want <= %d", source.maxRead, FileTransferChunkSize)
	}
	if terminal.maxPayload > FileTransferChunkSize {
		t.Errorf("largest Session payload = %d, want <= %d", terminal.maxPayload, FileTransferChunkSize)
	}
}

// TestReceiveFileVerifiesSHA256 covers marker/payload coalescing and successful
// verification in the board-to-PC direction.
func TestReceiveFileVerifiesSHA256(t *testing.T) {
	content := bytes.Repeat([]byte("hash-me-\x00\xff"), FileTransferChunkSize/4)
	terminal := newFileTransferTestSession(content)
	var destination bytes.Buffer
	result, err := ReceiveFile(context.Background(), terminal, &destination, "/tmp/log.txt", nil)
	if err != nil {
		t.Fatalf("ReceiveFile() error = %v", err)
	}
	if !bytes.Equal(destination.Bytes(), content) {
		t.Fatal("ReceiveFile() destination differs from remote content")
	}
	wantDigest := sha256.Sum256(content)
	if result.SHA256 != hex.EncodeToString(wantDigest[:]) {
		t.Errorf("ReceiveFile() SHA-256 = %s, want %x", result.SHA256, wantDigest)
	}
}

// TestReceiveFileRejectsChecksumMismatch verifies corrupted or changing remote
// metadata cannot be reported as a successful transfer.
func TestReceiveFileRejectsChecksumMismatch(t *testing.T) {
	terminal := newFileTransferTestSession([]byte("remote data"))
	terminal.badMetadataHash = true
	var destination bytes.Buffer
	_, err := ReceiveFile(context.Background(), terminal, &destination, "/tmp/log.txt", nil)
	if !errors.Is(err, ErrFileTransferChecksumMismatch) {
		t.Fatalf("ReceiveFile() error = %v, want ErrFileTransferChecksumMismatch", err)
	}
}

// TestSendFileReportsRemoteInitializationFailure verifies missing or
// incompatible Linux commands fail before any payload is sent.
func TestSendFileReportsRemoteInitializationFailure(t *testing.T) {
	terminal := newFileTransferTestSession(nil)
	terminal.failInitialization = true
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := SendFile(ctx, terminal, strings.NewReader("data"), 4, "/read-only/file", nil)
	if err == nil || !strings.Contains(err.Error(), "remote file transfer failed: tools") {
		t.Fatalf("SendFile() error = %v, want remote tools failure", err)
	}
}

// TestSendFilePadsInterruptedChunk verifies a partial Session write does not
// leave the board-side dd command waiting in raw TTY mode.
func TestSendFilePadsInterruptedChunk(t *testing.T) {
	terminal := newFileTransferTestSession(nil)
	terminal.failPayloadOnce = true
	_, err := SendFile(context.Background(), terminal, strings.NewReader("payload"), 7, "/tmp/partial.bin", nil)
	if err == nil || !strings.Contains(err.Error(), "injected payload failure") {
		t.Fatalf("SendFile() error = %v, want injected payload failure", err)
	}
	if terminal.pendingSend != 0 {
		t.Errorf("remote dd still waits for %d bytes after cleanup", terminal.pendingSend)
	}
}

type generatedFileReader struct {
	remaining int64
	offset    int64
	maxRead   int
}

func (r *generatedFileReader) Read(buffer []byte) (int, error) {
	if len(buffer) > r.maxRead {
		r.maxRead = len(buffer)
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	count := len(buffer)
	if int64(count) > r.remaining {
		count = int(r.remaining)
	}
	for index := range count {
		buffer[index] = byte((r.offset + int64(index)) % 251)
	}
	r.offset += int64(count)
	r.remaining -= int64(count)
	return count, nil
}

var (
	fileTestTokenPattern = regexp.MustCompile(`t='([0-9a-f]+)'`)
	fileTestSizePattern  = regexp.MustCompile(`; n=([0-9]+);`)
	fileTestIndexPattern = regexp.MustCompile(`; i=([0-9]+);`)
)

type fileTransferTestSession struct {
	mu sync.Mutex

	output []byte
	notify chan struct{}

	token              string
	received           []byte
	remote             []byte
	pendingSend        int
	pendingSendTotal   int
	maxPayload         int
	badMetadataHash    bool
	failInitialization bool
	failPayloadOnce    bool
	receiving          bool
}

func newFileTransferTestSession(remote []byte) *fileTransferTestSession {
	return &fileTransferTestSession{remote: append([]byte(nil), remote...), notify: make(chan struct{})}
}

func (s *fileTransferTestSession) ReadRecent(_ context.Context, maxBytes int) (session.OutputChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := len(s.output) - maxBytes
	if start < 0 {
		start = 0
	}
	return session.OutputChunk{Data: append([]byte(nil), s.output[start:]...), Next: session.OutputCursor(len(s.output))}, nil
}

func (s *fileTransferTestSession) ReadOutput(ctx context.Context, next session.OutputCursor, maxBytes int) (session.OutputChunk, error) {
	for {
		s.mu.Lock()
		if int(next) < len(s.output) {
			end := int(next) + maxBytes
			if end > len(s.output) {
				end = len(s.output)
			}
			data := append([]byte(nil), s.output[int(next):end]...)
			s.mu.Unlock()
			return session.OutputChunk{Data: data, Next: session.OutputCursor(end)}, nil
		}
		notify := s.notify
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return session.OutputChunk{}, ctx.Err()
		case <-notify:
		}
	}
}

func (s *fileTransferTestSession) Write(request session.WriteRequest) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := request.Data
	if s.pendingSend > 0 {
		count := len(data)
		if count > s.pendingSend {
			count = s.pendingSend
		}
		injectedFailure := s.failPayloadOnce
		if injectedFailure {
			s.failPayloadOnce = false
			count /= 2
			if count == 0 {
				count = 1
			}
		}
		s.received = append(s.received, data[:count]...)
		if count > s.maxPayload {
			s.maxPayload = count
		}
		s.pendingSend -= count
		if s.pendingSend == 0 {
			s.emitLocked(fmt.Sprintf("\n@CTERM:%s:ACK:%d\n", s.token, s.pendingSendTotal))
		}
		if injectedFailure {
			return count, errors.New("injected payload failure")
		}
		return count, nil
	}
	command := string(data)
	tokenMatch := fileTestTokenPattern.FindStringSubmatch(command)
	if len(tokenMatch) != 2 {
		return 0, errors.New("test shell command has no transfer token")
	}
	s.token = tokenMatch[1]
	switch {
	case strings.Contains(command, ":INIT:OK"):
		s.received = nil
		if s.failInitialization {
			s.emitLocked(fmt.Sprintf("\n@CTERM:%s:ERROR:tools\n", s.token))
		} else {
			s.emitLocked(fmt.Sprintf("\n@CTERM:%s:INIT:OK\n", s.token))
		}
	case strings.Contains(command, ":READY:"):
		size := mustFileTestNumber(command, fileTestSizePattern)
		s.pendingSend = size
		s.pendingSendTotal = size
		s.emitLocked(fmt.Sprintf("\n@CTERM:%s:READY:%d\n", s.token, size))
	case strings.Contains(command, ":FINAL:"):
		content := s.received
		if s.receiving {
			content = s.remote
		}
		digest := sha256.Sum256(content)
		s.emitLocked(fmt.Sprintf("\n@CTERM:%s:FINAL:%d:%x\n", s.token, len(content), digest))
	case strings.Contains(command, ":META:"):
		s.receiving = true
		digest := sha256.Sum256(s.remote)
		digestText := hex.EncodeToString(digest[:])
		if s.badMetadataHash {
			digestText = strings.Repeat("0", sha256.Size*2)
		}
		s.emitLocked(fmt.Sprintf("\n@CTERM:%s:META:%d:%s\n", s.token, len(s.remote), digestText))
	case strings.Contains(command, ":DATA:"):
		index := mustFileTestNumber(command, fileTestIndexPattern)
		size := mustFileTestNumber(command, fileTestSizePattern)
		start := index * FileTransferChunkSize
		end := start + size
		s.emitLocked(fmt.Sprintf("\n@CTERM:%s:DATA:%d\n", s.token, size))
		s.emitLocked(string(s.remote[start:end]))
		s.emitLocked(fmt.Sprintf("\n@CTERM:%s:ACK:%d\n", s.token, size))
	default:
		return 0, fmt.Errorf("unrecognized test shell command: %s", command)
	}
	return len(data), nil
}

func (s *fileTransferTestSession) emitLocked(data string) {
	s.output = append(s.output, data...)
	close(s.notify)
	s.notify = make(chan struct{})
}

func mustFileTestNumber(command string, pattern *regexp.Regexp) int {
	match := pattern.FindStringSubmatch(command)
	if len(match) != 2 {
		panic("missing number in test command: " + command)
	}
	value, err := strconv.Atoi(match[1])
	if err != nil {
		panic(err)
	}
	return value
}
