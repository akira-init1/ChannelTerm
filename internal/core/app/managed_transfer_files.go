package app

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
)

type managedEntry struct {
	source string
	name   string
	info   fs.FileInfo
}

// scanManagedSources uses Root.FS for both traversal and later opens. Lexical
// containment alone would allow a racing symlink to expose Host files.
func scanManagedSources(ctx context.Context, root *os.Root, sources []string) ([]managedEntry, bool, error) {
	var entries []managedEntry
	singleFile := false
	for _, source := range sources {
		source = filepath.ToSlash(filepath.Clean(source))
		info, err := root.Lstat(filepath.FromSlash(source))
		if err != nil {
			return nil, false, err
		}
		singleFile = len(sources) == 1 && info.Mode().IsRegular()
		prefix := filepath.ToSlash(filepath.Base(source))
		if len(sources) == 1 && info.IsDir() {
			prefix = ""
		}
		err = fs.WalkDir(root.FS(), source, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf("%w: %s", ErrUnsupportedDirectoryEntry, current)
			}
			rel := strings.TrimPrefix(current, source)
			name := strings.TrimPrefix(prefix+rel, "/")
			if name == "" {
				return nil
			}
			if !fs.ValidPath(name) || strings.ContainsAny(name, "\\\r\n\x00") {
				return ErrUnsafeTarPath
			}
			entries = append(entries, managedEntry{source: current, name: name, info: info})
			if len(entries) > directoryManifestLimit {
				return errors.New("transfer exceeds 10000 entries")
			}
			return nil
		})
		if err != nil {
			return nil, false, err
		}
	}
	return entries, singleFile, nil
}

func managedFootprint(entries []managedEntry) (transferFootprint, error) {
	f := transferFootprint{entries: int64(len(entries)) + 1}
	for _, e := range entries {
		if !e.info.IsDir() {
			n := e.info.Size()
			if n < 0 || n > int64(^uint64(0)>>1)-f.bytes {
				return f, ErrTransferSpaceUnavailable
			}
			f.bytes += n
		}
	}
	return f, nil
}

// copyManagedEntries creates exclusively through rooted handles. Existing data
// is never truncated. The caller owns newly allocated staging/destination paths.
// Each file is synced and read back before it contributes to a successful tree.
func copyManagedEntries(ctx context.Context, src, dst *os.Root, entries []managedEntry, prefix string) error {
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := filepath.Join(prefix, filepath.FromSlash(e.name))
		if e.info.IsDir() {
			if err := dst.Mkdir(name, 0700); err != nil {
				return err
			}
			continue
		}
		input, err := src.Open(filepath.FromSlash(e.source))
		if err != nil {
			return err
		}
		info, statErr := input.Stat()
		if statErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, e.info) || info.Size() != e.info.Size() {
			_ = input.Close()
			return errors.Join(errors.New("source changed during transfer preparation"), statErr)
		}
		output, err := dst.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600|info.Mode().Perm()&0111)
		if err != nil {
			_ = input.Close()
			return err
		}
		hash := sha256.New()
		_, copyErr := copySSHFile(ctx, io.MultiWriter(output, hash), input, e.name, info.Size(), SSHFilesRequest{})
		err = errors.Join(copyErr, input.Close(), output.Sync(), output.Close())
		if err != nil {
			return err
		}
		actual, err := hashSSHFile(ctx, info.Size(), func() (io.ReadCloser, error) { return dst.Open(name) })
		if err != nil {
			return err
		}
		if actual != fmt.Sprintf("%x", hash.Sum(nil)) {
			return ErrFileTransferChecksumMismatch
		}
	}
	return nil
}

func createManagedArchive(ctx context.Context, source, target, mode string) error {
	plan, err := makeDirectoryPlanContext(ctx, source)
	if err != nil {
		return err
	}
	// gzip may expand incompressible bytes; reserve its worst-case overhead.
	if plan.size < 0 || plan.size > math.MaxInt64-plan.size/100-1024*1024 {
		return ErrTransferSpaceUnavailable
	}
	if err := checkLocalTransferSpace(target, transferFootprint{bytes: plan.size + plan.size/100 + 1024*1024, entries: 1}); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	var out io.Writer = &transferContextWriter{ctx: ctx, writer: f}
	var compressed *gzip.Writer
	if mode == "tar.gz" {
		compressed = gzip.NewWriter(out)
		out = compressed
	}
	err = plan.writeToContext(ctx, out)
	if compressed != nil {
		err = errors.Join(err, compressed.Close())
	}
	return errors.Join(err, f.Sync(), f.Close())
}

func (r *transferRun) execute(ctx context.Context, settings config.FileTransfer) (err error) {
	root, err := os.OpenRoot(settings.Root)
	if err != nil {
		return fmt.Errorf("open configured Host transfer root: %w", err)
	}
	defer root.Close()
	stage, err := os.MkdirTemp("", "channelterm-transfer-")
	if err != nil {
		return err
	}
	stageCleaned := false
	defer func() {
		if !stageCleaned {
			err = errors.Join(err, os.RemoveAll(stage))
		}
	}()
	tree := filepath.Join(stage, "tree")
	if err := os.Mkdir(tree, 0700); err != nil {
		return err
	}
	q := r.request
	var single bool
	if q.Direction == "send" {
		entries, isFile, err := scanManagedSources(ctx, root, q.Sources)
		if err != nil {
			return err
		}
		single = isFile
		footprint, err := managedFootprint(entries)
		if err != nil {
			return err
		}
		if err := checkLocalTransferSpace(tree, footprint); err != nil {
			return err
		}
		dst, err := os.OpenRoot(tree)
		if err != nil {
			return err
		}
		err = copyManagedEntries(ctx, root, dst, entries, "")
		err = errors.Join(err, dst.Close())
		if err != nil {
			return err
		}
	} else {
		// Detect collisions and invalid parents before opening a remote endpoint.
		if _, err := root.Lstat(q.Destination); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				err = errors.New("Host destination already exists")
			}
			return err
		}
		parent, err := root.Stat(filepath.Dir(q.Destination))
		if err != nil {
			return err
		}
		if !parent.IsDir() {
			return errors.New("Host destination parent must be a directory")
		}
	}
	r.update(func(s *TransferStatus) { s.Phase = "transfer" })
	if q.Transport == "serial" {
		err = r.withSerial(ctx, func(ctx context.Context, terminal FileTransferSession) (operationErr error) {
			// Finish private cleanup before publishing the legacy completion event.
			defer func() { operationErr = errors.Join(operationErr, os.RemoveAll(stage)); stageCleaned = true }()
			if q.Direction == "receive" {
				var receiveErr error
				single, receiveErr = r.receiveSerial(ctx, terminal, tree, settings.Root)
				if receiveErr != nil {
					return receiveErr
				}
				payload, isFile, err := r.preparePayload(ctx, tree, stage, single)
				if err != nil {
					return err
				}
				return r.publishLocal(ctx, root, settings.Root, payload, isFile)
			}
			payload, isFile, err := r.preparePayload(ctx, tree, stage, single)
			if err != nil {
				return err
			}
			return r.sendSerial(ctx, terminal, payload, isFile)
		})
	} else {
		if q.Direction == "receive" {
			single, err = r.receiveSSH(ctx, tree, settings)
		} else {
			var payload string
			var isFile bool
			payload, isFile, err = r.preparePayload(ctx, tree, stage, single)
			if err == nil {
				err = r.sendSSH(ctx, payload, isFile, settings)
			}
		}
	}
	if err != nil {
		return err
	}
	if q.Direction == "receive" && q.Transport != "serial" {
		payload, isFile, err := r.preparePayload(ctx, tree, stage, single)
		if err != nil {
			return err
		}
		r.update(func(s *TransferStatus) { s.Phase = "publish" })
		return r.publishLocal(ctx, root, settings.Root, payload, isFile)
	}
	return nil
}

func (r *transferRun) preparePayload(ctx context.Context, tree, stage string, single bool) (string, bool, error) {
	m, err := localDirectoryManifest(ctx, tree)
	if err != nil {
		return "", false, err
	}
	result := m.result("")
	r.update(func(s *TransferStatus) { s.TotalBytes, s.FilesTotal = result.Bytes, result.Files })
	if r.request.Mode == "tar" || r.request.Mode == "tar.gz" {
		archive := filepath.Join(stage, "bundle."+r.request.Mode)
		if err := createManagedArchive(ctx, tree, archive, r.request.Mode); err != nil {
			return "", false, err
		}
		info, err := os.Stat(archive)
		if err != nil {
			return "", false, err
		}
		r.update(func(s *TransferStatus) {
			s.Mode, s.Reason = r.request.Mode, "explicit archive output"
			s.TotalBytes, s.FilesTotal = info.Size(), 1
		})
		return archive, true, nil
	}
	r.update(func(s *TransferStatus) {
		s.Mode, s.Reason = "files", "preserve the selected files and directory structure"
	})
	if single {
		entries, err := os.ReadDir(tree)
		if err != nil {
			return "", false, err
		}
		if len(entries) != 1 || entries[0].IsDir() {
			return "", false, errors.New("single-file staging layout changed")
		}
		return filepath.Join(tree, entries[0].Name()), true, nil
	}
	return tree, false, nil
}

func (r *transferRun) publishLocal(ctx context.Context, root *os.Root, rootPath, payload string, single bool) (err error) {
	src, err := os.OpenRoot(filepath.Dir(payload))
	if err != nil {
		return err
	}
	defer src.Close()
	entries, _, err := scanManagedSources(ctx, src, []string{filepath.Base(payload)})
	if err != nil {
		return err
	}
	footprint, err := managedFootprint(entries)
	if err != nil {
		return err
	}
	if err := checkLocalTransferSpace(filepath.Join(rootPath, r.request.Destination), footprint); err != nil {
		return err
	}
	dest := r.request.Destination
	if single {
		// A file has one root-relative destination rather than a directory prefix.
		entries[0].name = filepath.ToSlash(dest)
		err = copyManagedEntries(ctx, src, root, entries, "")
		// Do not remove a pre-existing or concurrently created destination on error.
		// An exclusive partial file remains at this exact reported path.
	} else {
		if err := root.Mkdir(dest, 0700); err != nil {
			return err
		}
		err = copyManagedEntries(ctx, src, root, entries, dest)
	}
	r.update(func(s *TransferStatus) { s.ResolvedPath = dest })
	if err != nil {
		return fmt.Errorf("publish Host destination %q (partial data may remain): %w", dest, err)
	}
	var digest string
	var size int64
	if single {
		size, digest, err = hashTransferFile(ctx, payload, -1)
	} else {
		var m directoryManifest
		m, err = localDirectoryManifest(ctx, payload)
		if err == nil {
			result := m.result("")
			size, digest = result.Bytes, result.SHA256
		}
	}
	if err != nil {
		return err
	}
	r.update(func(s *TransferStatus) {
		s.Bytes, s.SHA256, s.ChecksumScope = size, digest, "manifest"
		s.TotalBytes = size
		s.FilesVerified = s.FilesTotal
		if single {
			s.ChecksumScope = "file"
		}
		if r.request.Mode == "tar" || r.request.Mode == "tar.gz" {
			s.ChecksumScope = "archive"
		}
	})
	return nil
}
