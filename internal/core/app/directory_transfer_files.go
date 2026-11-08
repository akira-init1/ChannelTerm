package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DirectoryFileProgress reports original file bytes and files whose destination
// hash has been verified. Returning an error cancels at a safe transfer boundary.
type DirectoryFileProgress func(name string, verifiedFiles, totalFiles int, transferred, total int64) error

// DirectoryFilesResult describes a verified tree. SHA256 is the canonical
// manifest digest, not a TAR digest; Bytes excludes directory/manifest metadata.
type DirectoryFilesResult struct {
	// Bytes counts original regular-file contents, excluding metadata.
	Bytes int64
	// Files counts regular files verified by destination readback.
	Files int
	// SHA256 identifies the sorted, verified manifest.
	SHA256 string
	// RemotePath is the selected upload destination or requested download source.
	RemotePath string
}

type directoryManifestEntry struct {
	Name   string `json:"path"`
	Kind   string `json:"kind"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

type directoryManifest []directoryManifestEntry

// Limits bound metadata retained from an untrusted remote tree. Per-file mode
// supports the same line-safe paths as ordinary file commands, including spaces.
const directoryManifestLimit = 10000
const directoryManifestNameLimit = 8 * 1024 * 1024

func (m directoryManifest) validate() error {
	if len(m) > directoryManifestLimit {
		return errors.New("directory exceeds the 10000-entry per-file limit")
	}
	sort.Slice(m, func(i, j int) bool { return m[i].Name < m[j].Name })
	seen := map[string]string{}
	nameBytes := 0
	var total int64
	for _, e := range m {
		nameBytes += len(e.Name)
		if nameBytes > directoryManifestNameLimit || !utf8.ValidString(e.Name) || e.Name == "." || path.Clean(e.Name) != e.Name || !filepath.IsLocal(filepath.FromSlash(e.Name)) || strings.ContainsAny(e.Name, "\\\x00\r\n") {
			return fmt.Errorf("%w: per-file path %q", ErrUnsafeTarPath, e.Name)
		}
		if _, ok := seen[e.Name]; ok {
			return fmt.Errorf("duplicate directory entry %q", e.Name)
		}
		if parent := path.Dir(e.Name); parent != "." && seen[parent] != "directory" {
			return fmt.Errorf("missing directory parent for %q", e.Name)
		}
		if e.Size < 0 || total > math.MaxInt64-e.Size {
			return ErrFileTransferSizeMismatch
		}
		switch e.Kind {
		case "directory":
			if e.Size != 0 || e.SHA256 != "" {
				return ErrFileTransferProtocol
			}
		case "file":
			if !checkpointDigest.MatchString(e.SHA256) {
				return ErrFileTransferProtocol
			}
			total += e.Size
		default:
			return fmt.Errorf("%w: %q", ErrUnsupportedDirectoryEntry, e.Name)
		}
		seen[e.Name] = e.Kind
	}
	return nil
}

func (m directoryManifest) footprint() transferFootprint {
	f := transferFootprint{entries: int64(len(m)) + 1}
	for _, e := range m {
		f.bytes += e.Size
	}
	return f
}

func (m directoryManifest) result(remote string) DirectoryFilesResult {
	data, _ := json.Marshal(m)
	digest := sha256.Sum256(data)
	r := DirectoryFilesResult{Bytes: m.footprint().bytes, SHA256: hex.EncodeToString(digest[:]), RemotePath: remote}
	for _, e := range m {
		if e.Kind == "file" {
			r.Files++
		}
	}
	return r
}

func compareDirectoryManifests(want, got directoryManifest) error {
	if len(want) != len(got) {
		return fmt.Errorf("%w: directory entry count changed (%d versus %d)", ErrFileTransferChecksumMismatch, len(want), len(got))
	}
	for i, e := range want {
		if e != got[i] {
			return fmt.Errorf("%w: directory entry %q differs", ErrFileTransferChecksumMismatch, e.Name)
		}
	}
	return nil
}

func localDirectoryManifest(ctx context.Context, source string) (directoryManifest, error) {
	m := directoryManifest{}
	err := filepath.WalkDir(source, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == source {
			if !entry.IsDir() {
				return errors.New("per-file source must be a directory")
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		e := directoryManifestEntry{Name: filepath.ToSlash(rel), Kind: "directory"}
		if info.Mode().IsRegular() {
			e.Kind = "file"
			e.Size, e.SHA256, err = hashTransferFile(ctx, name, -1)
			if err != nil {
				return err
			}
		} else if !info.IsDir() {
			return fmt.Errorf("%w: %q", ErrUnsupportedDirectoryEntry, name)
		}
		m = append(m, e)
		if len(m) > directoryManifestLimit {
			return errors.New("directory exceeds the 10000-entry per-file limit")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return m, nil
}

func (p *fileProtocol) directoryManifest(ctx context.Context, source string) (directoryManifest, error) {
	if err := fileTransferCancellationCheckpoint(ctx, p.terminal, nil); err != nil {
		return nil, err
	}
	quoted, err := quoteRemotePath(source)
	if err != nil {
		return nil, err
	}
	command := directoryManifestCommand(p.token, quoted)
	if len(command) > 3500 {
		return nil, errors.New("directory path is too long for per-file command framing")
	}
	if err := p.beginDirectoryCommand(ctx, command); err != nil {
		return nil, err
	}
	m := directoryManifest{}
	nameBytes := 0
	for {
		fields, err := p.expectPhase(ctx, "TREE")
		if err != nil {
			return nil, err
		}
		if len(fields) == 1 && fields[0] == "DONE" {
			if shell, ok := p.terminal.(*shellFileTransferSession); ok {
				shell.commandCompleted()
			}
			if err := m.validate(); err != nil {
				return nil, err
			}
			return m, nil
		}
		if len(fields) != 4 || len(m) >= directoryManifestLimit {
			return nil, ErrFileTransferProtocol
		}
		name, err := hex.DecodeString(fields[3])
		if err != nil {
			return nil, ErrFileTransferProtocol
		}
		nameBytes += len(name)
		if nameBytes > directoryManifestNameLimit {
			return nil, ErrFileTransferProtocol
		}
		n, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
		if err != nil {
			return nil, ErrFileTransferProtocol
		}
		digest := fields[2]
		if digest == "-" {
			digest = ""
		}
		m = append(m, directoryManifestEntry{Name: string(name), Kind: fields[0], Size: n, SHA256: digest})
	}
}

// Names are hex encoded, hashes use stdin so filename quoting cannot alter the
// record, and only the final DONE re-enables the shell heartbeat.
func directoryManifestCommand(token, quoted string) string {
	return fmt.Sprintf(`t='%s'; root=%s; if [ ! -L "$root" ] && (cd -- "$root" && command -v od >/dev/null && command -v tr >/dev/null && command -v sha256sum >/dev/null && find . -exec sh -c 't=$1; shift; for f do [ "$f" != . ] || continue; k=other; n=0; h=-; if [ -L "$f" ]; then :; elif [ -d "$f" ]; then k=directory; elif [ -f "$f" ]; then k=file; n=$(wc -c < "$f") || exit 1; h=$(sha256sum < "$f") || exit 1; h=${h%%%% *}; fi; v=$(printf "%%s" "${f#./}" | od -An -tx1 | tr -d " \n"); printf "\n@CTERM:%%s:TREE:%%s:%%s:%%s:%%s\n" "$t" "$k" "$n" "$h" "$v"; done' sh "$t" {} +); then printf '\n@CTERM:%%s:TREE:DONE\n' "$t"; else printf '\n@CTERM:%%s:ERROR:directory-manifest\n' "$t"; fi`, token, quoted)
}

// beginDirectoryCommand starts an independent control operation at the current
// tail. Nested SendFile/ReceiveFile calls consume and verify their own responses;
// retaining our earlier cursor across them would mistake expired history for
// output loss. Never reset a cursor during a command or an active payload block.
func (p *fileProtocol) beginDirectoryCommand(ctx context.Context, command string) error {
	if shell, ok := p.terminal.(*shellFileTransferSession); ok {
		if err := shell.failedRecovery(); err != nil {
			return err
		}
	}
	recent, err := p.terminal.ReadRecent(ctx, 1)
	if err != nil {
		return fmt.Errorf("initialize directory command cursor: %w", err)
	}
	p.cursor, p.pending = recent.Next, nil
	return p.command(ctx, command)
}

func (p *fileProtocol) directoryFilesCommand(ctx context.Context, body string) error {
	command := fmt.Sprintf("t='%s'; if ( %s ); then printf '\\n@CTERM:%%s:FILES:OK\\n' \"$t\"; else printf '\\n@CTERM:%%s:ERROR:directory-files\\n' \"$t\"; fi", p.token, body)
	if len(command) > 3500 {
		return errors.New("directory path is too long for per-file command framing")
	}
	if err := p.beginDirectoryCommand(ctx, command); err != nil {
		return err
	}
	_, err := p.expect(ctx, "FILES", "OK")
	return err
}

// SendDirectoryFiles sends a directory without a remote TAR. The caller holds
// one transfer lease throughout planning, copying, readback and publication.
// A private staging tree is published only after both manifests still match.
func SendDirectoryFiles(ctx context.Context, terminal FileTransferSession, source, remote string, progress DirectoryFileProgress) (result DirectoryFilesResult, err error) {
	m, err := localDirectoryManifest(ctx, source)
	if err != nil {
		return result, err
	}
	err = WithFileTransferShell(ctx, terminal, func(scoped FileTransferSession) (operationErr error) {
		p, err := newFileProtocol(ctx, scoped)
		if err != nil {
			return err
		}
		remote = strings.TrimSuffix(remote, "/")
		remote, quoted, err := p.selectAvailableRemotePath(ctx, remote)
		if err != nil {
			return err
		}
		if err := p.checkRemoteTransferSpace(ctx, remote, m.footprint()); err != nil {
			return err
		}
		if err := p.directoryFilesCommand(ctx, `for cmd in find od tr sha256sum wc; do command -v "$cmd" >/dev/null || exit 1; done`); err != nil {
			return fmt.Errorf("per-file mode requires find, od, tr, sha256sum and wc: %w", err)
		}
		stage := posixDirectoryStagePath(remote, p.token)
		qs, _ := quoteRemotePath(stage)
		parent, _ := quoteRemotePath(path.Dir(remote))
		// Probe non-replacing, non-nesting publication before file payload. The
		// random directory is cleaned only after our exclusive mkdir succeeds.
		init := fmt.Sprintf(`s=%s; umask 077; mkdir -p %s && mkdir "$s" && { if mkdir "$s/probe" "$s/probe/from" "$s/probe/to" && { mv -nT "$s/probe/from" "$s/probe/to"; [ -d "$s/probe/from" ] && [ ! -e "$s/probe/to/from" ]; } && rmdir "$s/probe/to" && mv -nT "$s/probe/from" "$s/probe/to" && [ ! -e "$s/probe/from" ] && rmdir "$s/probe/to" "$s/probe"; then :; else rm -rf "$s"; false; fi; }`, qs, parent)
		if err := p.directoryFilesCommand(ctx, init); err != nil {
			return err
		}
		published := false
		defer func() {
			if !published {
				cleanupCtx, cancel := fileTransferRecoveryContext()
				defer cancel()
				cleanupErr := p.directoryFilesCommand(cleanupCtx, "rm -rf "+qs)
				operationErr = errors.Join(operationErr, p.recoveryError(cleanupErr))
			}
		}()
		root, err := os.OpenRoot(source)
		if err != nil {
			return err
		}
		defer root.Close()
		result = m.result(remote)
		var transferred int64
		verified := 0
		for _, e := range m {
			if err := fileTransferCancellationCheckpoint(ctx, scoped, nil); err != nil {
				return err
			}
			target := path.Join(stage, e.Name)
			qt, err := quoteRemotePath(target)
			if err != nil {
				return err
			}
			if e.Kind == "directory" {
				if err := p.directoryFilesCommand(ctx, "mkdir "+qt); err != nil {
					return err
				}
				continue
			}
			if progress != nil {
				if err := progress(e.Name, verified, result.Files, transferred, result.Bytes); err != nil {
					return err
				}
			}
			f, err := root.Open(filepath.FromSlash(e.Name))
			if err != nil {
				return err
			}
			r, copyErr := SendFile(ctx, scoped, f, e.Size, target, func(n, _ int64) error {
				if progress != nil {
					return progress(e.Name, verified, result.Files, transferred+n, result.Bytes)
				}
				return nil
			})
			closeErr := f.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return fmt.Errorf("send directory file %q: %w", e.Name, err)
			}
			if r.RemotePath != target || r.Size != e.Size || r.SHA256 != e.SHA256 {
				return fmt.Errorf("%w: source or destination changed for %q", ErrFileTransferChecksumMismatch, e.Name)
			}
			transferred += e.Size
			verified++
			if progress != nil {
				if err := progress(e.Name, verified, result.Files, transferred, result.Bytes); err != nil {
					return err
				}
			}
		}
		got, err := p.directoryManifest(ctx, stage)
		if err != nil {
			return err
		}
		if err := compareDirectoryManifests(m, got); err != nil {
			return err
		}
		current, err := localDirectoryManifest(ctx, source)
		if err != nil {
			return err
		}
		if err := compareDirectoryManifests(m, current); err != nil {
			return err
		}
		if err := p.checkRemoteTransferSpace(ctx, stage, transferFootprint{}); err != nil {
			return err
		}
		if err := fileTransferCancellationCheckpoint(ctx, scoped, nil); err != nil {
			return err
		}
		if err := p.directoryFilesCommand(ctx, fmt.Sprintf("mv -nT %s %s && [ ! -e %s ]", qs, quoted, qs)); err != nil {
			var remoteErr *fileTransferRemoteError
			if !errors.As(err, &remoteErr) {
				return errors.Join(ErrTransferPublicationUncertain, err)
			}
			return err
		}
		published = true
		return nil
	})
	return result, err
}

// PublishReceivedDirectory exclusively renames a caller-owned staging directory
// on the same volume. A racing destination is never replaced or nested into.
// The caller must have completed verification and owns cleanup after failure.
func PublishReceivedDirectory(staging, destination string) error {
	info, err := os.Lstat(staging)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("received directory staging path must be a real directory")
	}
	return publishTransferPath(staging, destination)
}

// ReceiveDirectoryFiles copies into an empty caller-owned staging directory.
// It verifies destination readback and the complete source/destination manifests;
// callers publish the directory only after success and own local error cleanup.
func ReceiveDirectoryFiles(ctx context.Context, terminal FileTransferSession, remote, destination string, progress DirectoryFileProgress) (result DirectoryFilesResult, err error) {
	entries, err := os.ReadDir(destination)
	if err != nil {
		return result, err
	}
	if len(entries) != 0 {
		return result, errors.New("per-file destination staging directory must be empty")
	}
	err = WithFileTransferShell(ctx, terminal, func(scoped FileTransferSession) error {
		p, err := newFileProtocol(ctx, scoped)
		if err != nil {
			return err
		}
		m, err := p.directoryManifest(ctx, remote)
		if err != nil {
			return err
		}
		if err := checkLocalTransferSpace(destination, m.footprint()); err != nil {
			return err
		}
		root, err := os.OpenRoot(destination)
		if err != nil {
			return err
		}
		defer root.Close()
		result = m.result(remote)
		var transferred int64
		verified := 0
		for _, e := range m {
			if err := fileTransferCancellationCheckpoint(ctx, scoped, nil); err != nil {
				return err
			}
			name := filepath.FromSlash(e.Name)
			if e.Kind == "directory" {
				if err := root.Mkdir(name, 0700); err != nil {
					return err
				}
				continue
			}
			f, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			r, copyErr := ReceiveFile(ctx, scoped, f, path.Join(remote, e.Name), func(n, _ int64) error {
				if progress != nil {
					return progress(e.Name, verified, result.Files, transferred+n, result.Bytes)
				}
				return nil
			})
			if copyErr == nil && (r.Size != e.Size || r.SHA256 != e.SHA256) {
				copyErr = fmt.Errorf("%w: source changed for %q", ErrFileTransferChecksumMismatch, e.Name)
			}
			if copyErr == nil {
				copyErr = f.Sync()
			}
			if copyErr == nil {
				_, copyErr = f.Seek(0, io.SeekStart)
			}
			if copyErr == nil {
				h := sha256.New()
				var n int64
				n, copyErr = io.Copy(h, transferContextReader{ctx: ctx, reader: f})
				if copyErr == nil && (n != e.Size || hex.EncodeToString(h.Sum(nil)) != e.SHA256) {
					copyErr = fmt.Errorf("%w: destination readback %q", ErrFileTransferChecksumMismatch, e.Name)
				}
			}
			if err := errors.Join(copyErr, f.Close()); err != nil {
				return err
			}
			transferred += e.Size
			verified++
			if progress != nil {
				if err := progress(e.Name, verified, result.Files, transferred, result.Bytes); err != nil {
					return err
				}
			}
		}
		got, err := localDirectoryManifest(ctx, destination)
		if err != nil {
			return err
		}
		if err := compareDirectoryManifests(m, got); err != nil {
			return err
		}
		current, err := p.directoryManifest(ctx, remote)
		if err != nil {
			return err
		}
		return compareDirectoryManifests(m, current)
	})
	return result, err
}
