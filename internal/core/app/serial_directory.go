package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

// SerialDirectory is a bounded snapshot of an idle shell's directory, not a filesystem mount.
type SerialDirectory struct {
	Path      string                 `json:"path"`
	Entries   []SerialDirectoryEntry `json:"entries"`
	Truncated bool                   `json:"truncated"`
}

// SerialDirectoryEntry describes a direct child. Links and special files are not transferable.
type SerialDirectoryEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// ListSerialDirectory uses the existing leased transfer shell and independent output cursor.
// A dot path inherits the interactive shell's cwd; cd occurs only in the child shell.
// Callers must hold a file-transfer lease and supply a deadline at a known idle POSIX prompt.
func ListSerialDirectory(ctx context.Context, terminal FileTransferSession, path string) (result SerialDirectory, err error) {
	quoted, err := quoteRemotePath(path)
	if err != nil || len(path) > 4096 {
		return result, fmt.Errorf("invalid directory path")
	}
	result.Entries = []SerialDirectoryEntry{}
	err = WithFileTransferShell(ctx, terminal, func(scoped FileTransferSession) error {
		p, err := newFileProtocol(ctx, scoped)
		if err != nil {
			return err
		}
		if err = fileTransferCancellationCheckpoint(ctx, scoped, nil); err != nil {
			return err
		}
		if err = p.command(ctx, serialDirectoryCommand(p.token, quoted)); err != nil {
			return err
		}
		for count := 0; count < 258; count++ {
			fields, readErr := p.expectPhase(ctx, "LIST")
			if readErr != nil {
				return readErr
			}
			if len(fields) == 2 && fields[0] == "DONE" && result.Path != "" {
				if fields[1] != "0" && fields[1] != "1" {
					return ErrFileTransferProtocol
				}
				if shell, ok := scoped.(*shellFileTransferSession); ok {
					shell.commandCompleted()
				}
				result.Truncated = fields[1] == "1"
				return nil
			}
			if len(fields) != 2 {
				return ErrFileTransferProtocol
			}
			value, decodeErr := hex.DecodeString(fields[1])
			if decodeErr != nil || !utf8.Valid(value) || len(value) == 0 || len(value) > 4096 || strings.ContainsAny(string(value), "\x00\r\n") {
				return ErrFileTransferProtocol
			}
			if fields[0] == "CWD" && count == 0 && value[0] == '/' {
				result.Path = string(value)
			} else if result.Path != "" && (fields[0] == "file" || fields[0] == "directory" || fields[0] == "other") && !strings.Contains(string(value), "/") && string(value) != "." && string(value) != ".." {
				result.Entries = append(result.Entries, SerialDirectoryEntry{Name: string(value), Kind: fields[0]})
			} else {
				return ErrFileTransferProtocol
			}
		}
		return ErrFileTransferProtocol
	})
	return result, err
}

// Hex-encoded records preserve whitespace and Unicode without parsing ls output.
// Hidden entries are included; symlinks are classified before their referents.
func serialDirectoryCommand(token, quoted string) string {
	return fmt.Sprintf(`t='%s'; if ! command -v od >/dev/null 2>&1 || ! command -v tr >/dev/null 2>&1; then printf '\n@CTERM:%%s:ERROR:tools\n' "$t"; elif ! cd -- %s 2>/dev/null || [ ! -r . ] || [ ! -x . ]; then printf '\n@CTERM:%%s:ERROR:directory\n' "$t"; else p=$(pwd -P); h=$(printf '%%s' "$p" | od -An -tx1 | tr -d ' \n'); printf '\n@CTERM:%%s:LIST:CWD:%%s\n' "$t" "$h"; n=0; more=0; for f in ./* ./.[!.]* ./..?*; do [ -e "$f" ] || [ -L "$f" ] || continue; if [ "$n" -ge 256 ]; then more=1; break; fi; k=other; if [ -L "$f" ]; then :; elif [ -d "$f" ]; then k=directory; elif [ -f "$f" ]; then k=file; fi; h=$(printf '%%s' "${f#./}" | od -An -tx1 | tr -d ' \n'); printf '\n@CTERM:%%s:LIST:%%s:%%s\n' "$t" "$k" "$h"; n=$((n+1)); done; printf '\n@CTERM:%%s:LIST:DONE:%%s\n' "$t" "$more"; fi`, token, quoted)
}
