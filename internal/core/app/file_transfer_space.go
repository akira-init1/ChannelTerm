package app

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	// ErrTransferSpaceUnavailable means capacity could not be established. No
	// payload should be started on the assumption that an unknown volume is free.
	ErrTransferSpaceUnavailable = errors.New("cannot check file transfer storage space")
	// ErrTransferSpaceInsufficient means a preflight estimate exceeds available
	// bytes or inodes. The check is not a reservation against concurrent writers.
	ErrTransferSpaceInsufficient = errors.New("insufficient file transfer storage space")
)

type transferStorage struct {
	available int64
	block     int64
	inodes    int64 // -1 when the filesystem does not report an inode limit
	volume    string
}

// transferFootprint describes additional live data, not existing source files.
// Charging one allocation block per entry bounds rounding and directory overhead;
// a further 5%, at least 1 MiB, leaves headroom without reserving device capacity.
type transferFootprint struct {
	bytes   int64
	entries int64
}

func (f transferFootprint) required(block int64) (int64, error) {
	if f.bytes < 0 || f.entries < 0 || block <= 0 || f.entries > (math.MaxInt64-f.bytes)/block {
		return 0, fmt.Errorf("%w: storage estimate overflow", ErrTransferSpaceUnavailable)
	}
	n := f.bytes + f.entries*block
	margin := max(int64(1024*1024), n/20)
	if n > math.MaxInt64-margin {
		return 0, fmt.Errorf("%w: storage estimate overflow", ErrTransferSpaceUnavailable)
	}
	return n + margin, nil
}

func checkTransferStorage(path string, f transferFootprint, storage transferStorage) error {
	needed, err := f.required(storage.block)
	if err != nil {
		return err
	}
	if storage.available < needed || (storage.inodes >= 0 && storage.inodes < f.entries) {
		return fmt.Errorf("%w at %q: need approximately %d additional bytes including reserve, available %d bytes; need %d entries, available inodes %d (unknown=-1); choose another destination or free space", ErrTransferSpaceInsufficient, path, needed, storage.available, f.entries, storage.inodes)
	}
	return nil
}

func existingStoragePath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for {
		if info, err := os.Stat(path); err == nil {
			if !info.IsDir() {
				path = filepath.Dir(path)
			}
			return path, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", os.ErrNotExist
		}
		path = parent
	}
}

func checkLocalTransferSpace(path string, f transferFootprint) error {
	storage, err := localTransferStorage(path)
	if err != nil {
		return fmt.Errorf("%w at %q: %w", ErrTransferSpaceUnavailable, path, err)
	}
	return checkTransferStorage(path, f, storage)
}

func (p *fileProtocol) checkRemoteTransferSpace(ctx context.Context, path string, f transferFootprint) error {
	if err := fileTransferCancellationCheckpoint(ctx, p.terminal, nil); err != nil {
		return err
	}
	quoted, err := quoteRemotePath(path)
	if err != nil {
		return err
	}
	if err := p.command(ctx, remoteTransferSpaceCommand(p.token, quoted)); err != nil {
		return err
	}
	fields, err := p.expectPhase(ctx, "SPACE")
	if err != nil {
		return err
	}
	storage, err := parseTransferStorage(fields)
	if err != nil {
		return fmt.Errorf("%w at %q: target needs stat -f with filesystem format support", err, path)
	}
	return checkTransferStorage(path, f, storage)
}

func parseTransferStorage(fields []string) (transferStorage, error) {
	invalid := func() (transferStorage, error) { return transferStorage{}, ErrTransferSpaceUnavailable }
	if len(fields) != 4 {
		return invalid()
	}
	blocks, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || blocks < 0 {
		return invalid()
	}
	block, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || block <= 0 || blocks > math.MaxInt64/block {
		return invalid()
	}
	totalInodes, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil && fields[3] != "-1" {
		return invalid()
	}
	inodes := int64(-1)
	if totalInodes > 0 && totalInodes <= math.MaxInt64 {
		inodes, err = strconv.ParseInt(fields[2], 10, 64)
		if err != nil || inodes < 0 {
			return invalid()
		}
	}
	return transferStorage{available: blocks * block, block: block, inodes: inodes}, nil
}

// Resolve the nearest existing ancestor without creating the destination. stat
// queries that path's mounted filesystem, including a separate /tmp or tmpfs.
func remoteTransferSpaceCommand(token, quoted string) string {
	return fmt.Sprintf(`t='%s'; p=%s; case "$p" in /*) ;; *) p="$PWD/$p";; esac; while [ ! -e "$p" ]; do p=${p%%/*}; [ -n "$p" ] || p=/; done; if v=$(LC_ALL=C stat -f -c '%%a:%%S:%%d:%%c' "$p" 2>/dev/null); then printf '\n@CTERM:%%s:SPACE:%%s\n' "$t" "$v"; else printf '\n@CTERM:%%s:SPACE:UNKNOWN\n' "$t"; fi`, token, quoted)
}

// remoteDirectoryFootprint scans metadata only. Numeric records avoid parsing
// filenames, and a failed find/stat is propagated through the reducing pipeline.
func (p *fileProtocol) remoteDirectoryFootprint(ctx context.Context, source string) (transferFootprint, error) {
	if !strings.HasPrefix(source, "/") {
		source = "./" + source
	}
	quoted, err := quoteRemotePath(source)
	if err != nil {
		return transferFootprint{}, err
	}
	if err := p.command(ctx, remoteDirectoryFootprintCommand(p.token, quoted)); err != nil {
		return transferFootprint{}, err
	}
	fields, err := p.expectPhase(ctx, "FOOTPRINT")
	if err != nil {
		var remote *fileTransferRemoteError
		if errors.As(err, &remote) {
			return transferFootprint{}, fmt.Errorf("%w: directory metadata scan requires find, stat and awk, and regular files/directories only: %w", ErrTransferSpaceUnavailable, err)
		}
		return transferFootprint{}, err
	}
	if len(fields) != 2 {
		return transferFootprint{}, ErrTransferSpaceUnavailable
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || n < 0 {
		return transferFootprint{}, ErrTransferSpaceUnavailable
	}
	entries, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || entries < 1 {
		return transferFootprint{}, ErrTransferSpaceUnavailable
	}
	return transferFootprint{bytes: n, entries: entries}, nil
}

func remoteDirectoryFootprintCommand(token, quoted string) string {
	return fmt.Sprintf(`t='%s'; if v=$( (find %s -exec sh -c 'for p do if [ -L "$p" ]; then echo X; elif [ -d "$p" ]; then echo D; elif [ -f "$p" ]; then stat -c "F %%s" "$p" || echo X; else echo X; fi; done' sh {} + || echo X) | awk '$1=="F" && $2 ~ /^[0-9]+$/ {b+=$2; n++; next} $0=="D" {n++; next} {bad=1} END {if (bad || b>9007199254740991 || n>1000000 || n<1) exit 1; printf "%%.0f:%%.0f",b,n}'); then printf '\n@CTERM:%%s:FOOTPRINT:%%s\n' "$t" "$v"; else printf '\n@CTERM:%%s:ERROR:directory-size-unavailable\n' "$t"; fi`, token, quoted)
}

// A remote tar may include long-name headers and 10 KiB blocking. Use a
// conservative bound until its actual size is known; no compression is assumed.
func (f transferFootprint) tarBound() (int64, error) {
	if f.bytes < 0 || f.entries < 0 || f.entries > (math.MaxInt64-10240-f.bytes)/16384 {
		return 0, ErrTransferSpaceUnavailable
	}
	return f.bytes + f.entries*16384 + 10240, nil
}

func (p directoryPlan) footprint() transferFootprint {
	f := transferFootprint{entries: int64(len(p.entries)) + 1}
	for _, entry := range p.entries {
		f.bytes += entry.head.Size
	}
	return f
}

func archiveTransferFootprint(ctx context.Context, name string) (transferFootprint, error) {
	f, err := os.Open(name)
	if err != nil {
		return transferFootprint{}, err
	}
	defer f.Close()
	r := tar.NewReader(transferContextReader{ctx: ctx, reader: f})
	result := transferFootprint{entries: 1}
	for {
		header, err := r.Next()
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return transferFootprint{}, err
		}
		if header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return transferFootprint{}, ErrUnsupportedDirectoryEntry
		}
		if header.Size < 0 || result.bytes > math.MaxInt64-header.Size {
			return transferFootprint{}, ErrTransferSpaceUnavailable
		}
		result.bytes += header.Size
		result.entries++
	}
}

// Check simultaneously live local allocations once per volume. Snapshot and
// publication directories may share a volume even when their paths differ.
func checkLocalTransferAllocations(paths []string, footprints []transferFootprint) error {
	type allocation struct {
		path      string
		footprint transferFootprint
		storage   transferStorage
	}
	volumes := map[string]allocation{}
	for i, path := range paths {
		storage, err := localTransferStorage(path)
		if err != nil {
			return fmt.Errorf("%w at %q: %w", ErrTransferSpaceUnavailable, path, err)
		}
		a := volumes[storage.volume]
		if a.path == "" {
			a.path, a.storage = path, storage
		}
		if a.footprint.bytes > math.MaxInt64-footprints[i].bytes || a.footprint.entries > math.MaxInt64-footprints[i].entries {
			return ErrTransferSpaceUnavailable
		}
		a.footprint.bytes += footprints[i].bytes
		a.footprint.entries += footprints[i].entries
		volumes[storage.volume] = a
	}
	for _, a := range volumes {
		if err := checkTransferStorage(a.path, a.footprint, a.storage); err != nil {
			return err
		}
	}
	return nil
}

func (p *fileProtocol) checkCheckpointSpace(ctx context.Context, dir string, c TransferCheckpoint) error {
	footprint := transferFootprint{bytes: c.Size, entries: 1}
	if c.Kind == RemotePathDirectory {
		if c.Direction == "send" || c.Offset == c.Size {
			var err error
			footprint, err = archiveTransferFootprint(ctx, filepath.Join(dir, "payload"))
			if err != nil {
				return err
			}
		} else if c.StorageEntries > 0 {
			footprint = transferFootprint{bytes: c.StorageBytes, entries: c.StorageEntries}
		} else {
			// Older records lack the scan. A tar member uses at least one 512-byte
			// header; this upper bound is conservative until the archive is local.
			footprint.entries = c.Size/512 + 1
		}
	}
	remaining := c.Size - c.Offset
	if c.Direction == "send" {
		if c.Kind != RemotePathDirectory {
			footprint = transferFootprint{}
		}
		if footprint.bytes > math.MaxInt64-remaining {
			return ErrTransferSpaceUnavailable
		}
		footprint.bytes += remaining
		footprint.entries++
		return p.checkRemoteTransferSpace(ctx, checkpointRemoteDirectory(c), footprint)
	}
	return checkLocalTransferAllocations([]string{dir, c.LocalPath}, []transferFootprint{{bytes: remaining, entries: 1}, footprint})
}

// Explain known write failures without misclassifying permission or I/O errors
// as disk exhaustion. A capacity recheck may establish a more useful diagnosis.
func (p *fileProtocol) diagnoseRemoteStorage(ctx context.Context, path string, f transferFootprint, cause error) error {
	var remote *fileTransferRemoteError
	if !errors.As(cause, &remote) || (!strings.Contains(remote.reason, "write") && !strings.Contains(remote.reason, "extract")) {
		return cause
	}
	if err := p.checkRemoteTransferSpace(ctx, path, f); errors.Is(err, ErrTransferSpaceInsufficient) {
		return errors.Join(cause, err)
	}
	return cause
}
