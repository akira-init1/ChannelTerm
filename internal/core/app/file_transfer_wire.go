package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
)

// FileTransferWireEncoding selects framing only; file hashes and progress always
// describe original bytes. It never transcodes file contents as terminal text.
type FileTransferWireEncoding string

const (
	// FileTransferWireRaw preserves the original eight-bit protocol. XON/XOFF
	// must not be used with this mode because it consumes DC1/DC3 payload bytes.
	FileTransferWireRaw FileTransferWireEncoding = "raw"
	// FileTransferWireBase64 uses bounded ASCII commands, leaving peer TTY flow
	// settings intact. The peer must provide base64 with -d and POSIX tr.
	FileTransferWireBase64 FileTransferWireEncoding = "base64"
)

// fileTransferWireSelector is an optional client operation setting. Existing
// application callers retain raw behavior; public clients explicitly select it.
type fileTransferWireSelector interface {
	FileTransferWireEncoding() FileTransferWireEncoding
}

// FileTransferWireEncoding preserves the caller's framing inside the child shell.
func (s *shellFileTransferSession) FileTransferWireEncoding() FileTransferWireEncoding {
	if selector, ok := s.terminal.(fileTransferWireSelector); ok {
		return selector.FileTransferWireEncoding()
	}
	return FileTransferWireRaw
}

// ASCII records stay below common canonical TTY line limits. Each 2048-byte
// sub-block is acknowledged independently; no rejected/uncertain block retries.
const fileTransferASCIIBlockSize = 2048

func (p *fileProtocol) sendBlock(ctx context.Context, path string, chunk []byte) error {
	if p.wire == FileTransferWireBase64 {
		for offset := 0; offset < len(chunk); offset += fileTransferASCIIBlockSize {
			if err := fileTransferCancellationCheckpoint(ctx, p.terminal, nil); err != nil {
				return err
			}
			end := min(offset+fileTransferASCIIBlockSize, len(chunk))
			payload := base64.StdEncoding.EncodeToString(chunk[offset:end])
			command := fmt.Sprintf("t='%s'; p=%s; if before=$(wc -c < \"$p\") && printf '%%s' '%s' | base64 -d >> \"$p\" && after=$(wc -c < \"$p\") && [ \"$((after-before))\" = '%d' ]; then printf '\\n@CTERM:%%s:ACK:%d\\n' \"$t\"; else printf '\\n@CTERM:%%s:ERROR:write\\n' \"$t\"; fi", p.token, path, payload, end-offset, end-offset)
			if len(command) > 3500 {
				return errors.New("remote path is too long for ASCII transfer framing")
			}
			if err := p.command(ctx, command); err != nil {
				return err
			}
			if _, err := p.expect(ctx, "ACK", strconv.Itoa(end-offset)); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					err = errors.Join(err, p.finishPendingMarker("ACK", strconv.Itoa(end-offset)))
				}
				return err
			}
		}
		return nil
	}
	size := int64(len(chunk))
	if err := p.command(ctx, sendChunkCommand(p.token, path, size)); err != nil {
		return err
	}
	if _, err := p.expect(ctx, "READY", strconv.FormatInt(size, 10)); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			err = errors.Join(err, p.finishUnconfirmedSendChunk(size))
		}
		return err
	}
	written, err := writeFilePayload(ctx, p.terminal, chunk)
	if err != nil {
		return errors.Join(err, p.finishSendChunk(size-int64(written), size))
	}
	if _, err := p.expect(ctx, "ACK", strconv.FormatInt(size, 10)); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			err = errors.Join(err, p.finishPendingMarker("ACK", strconv.FormatInt(size, 10)))
		}
		return err
	}
	return nil
}

func (p *fileProtocol) receiveBlock(ctx context.Context, path string, blockIndex int64, chunk []byte) error {
	if p.wire == FileTransferWireBase64 {
		for offset := 0; offset < len(chunk); offset += fileTransferASCIIBlockSize {
			if err := fileTransferCancellationCheckpoint(ctx, p.terminal, nil); err != nil {
				return err
			}
			end := min(offset+fileTransferASCIIBlockSize, len(chunk))
			index := (blockIndex*FileTransferChunkSize + int64(offset)) / fileTransferASCIIBlockSize
			command := fmt.Sprintf("t='%s'; v=$(dd if=%s bs=%d skip=%d count=1 2>/dev/null | base64 | tr -d '\\r\\n'); printf '\\n@CTERM:%%s:B64:%%s\\n' \"$t\" \"$v\"", p.token, path, fileTransferASCIIBlockSize, index)
			if len(command) > 3500 {
				return errors.New("remote path is too long for ASCII transfer framing")
			}
			if err := p.command(ctx, command); err != nil {
				return err
			}
			event, err := p.expectPhase(ctx, "B64")
			if err != nil {
				return err
			}
			if len(event) != 1 || len(event[0]) != base64.StdEncoding.EncodedLen(end-offset) {
				return fmt.Errorf("%w: invalid Base64 block length", ErrFileTransferProtocol)
			}
			decoded, err := base64.StdEncoding.Strict().DecodeString(event[0])
			if err != nil || len(decoded) != end-offset {
				return fmt.Errorf("%w: invalid Base64 payload", ErrFileTransferProtocol)
			}
			copy(chunk[offset:end], decoded)
		}
		return nil
	}
	size := int64(len(chunk))
	if err := p.command(ctx, receiveChunkCommand(p.token, path, blockIndex, size)); err != nil {
		return err
	}
	if _, err := p.expect(ctx, "DATA", strconv.FormatInt(size, 10)); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			err = errors.Join(err, p.finishUnconfirmedReceiveChunk(size))
		}
		return err
	}
	read, err := p.readExact(ctx, chunk)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			err = errors.Join(err, p.finishReceiveChunk(chunk[read:], size))
		}
		return err
	}
	if _, err := p.expect(ctx, "ACK", strconv.FormatInt(size, 10)); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			err = errors.Join(err, p.finishPendingMarker("ACK", strconv.FormatInt(size, 10)))
		}
		return err
	}
	return nil
}
