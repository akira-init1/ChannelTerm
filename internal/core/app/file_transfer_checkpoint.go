package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/gofrs/flock"
)

var checkpointID = regexp.MustCompile(`^[a-f0-9]{24}$`)
var checkpointDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

// TransferCheckpoint describes a client-owned resumable copy. It contains no
// credentials or Session authority. Peer is an adapter-supplied endpoint binding;
// the caller must validate it against the explicitly selected live Session.
// Directory transfers use an immutable tar snapshot; offsets count archive bytes.
type TransferCheckpoint struct {
	Version      int                      `json:"version"`
	ID           string                   `json:"id"`
	Direction    string                   `json:"direction"`
	Kind         RemotePathKind           `json:"kind"`
	LocalPath    string                   `json:"local_path"`
	RemotePath   string                   `json:"remote_path"`
	Peer         string                   `json:"peer"`
	Wire         FileTransferWireEncoding `json:"wire"`
	Size         int64                    `json:"size"`
	SHA256       string                   `json:"sha256"`
	Offset       int64                    `json:"offset"`
	PrefixSHA256 string                   `json:"prefix_sha256"`
	// Phase is preparing, transferring, publishing, or completed. Publishing is
	// durable before any final rename, so an uncertain rename is never replayed.
	Phase string `json:"phase"`
}

// CreateTransferCheckpoint snapshots uploads before device access. Downloads
// reserve a local record; remote discovery runs later under the caller's Lease.
// root must be a private, client-owned directory, not a device-supplied path.
func CreateTransferCheckpoint(ctx context.Context, root, direction, localPath, remotePath, peer string, wire FileTransferWireEncoding) (TransferCheckpoint, error) {
	var c TransferCheckpoint
	if err := ctx.Err(); err != nil {
		return c, err
	}
	if direction != "send" && direction != "receive" {
		return c, errors.New("invalid resumable transfer direction")
	}
	if wire != FileTransferWireBase64 && wire != FileTransferWireRaw {
		return c, errors.New("invalid resumable transfer wire encoding")
	}
	if _, err := quoteRemotePath(remotePath); err != nil {
		return c, err
	}
	// Persistent jobs require absolute paths: a later shell may have another cwd.
	if remotePath[0] != '/' {
		return c, errors.New("resumable transfer requires an absolute device path")
	}
	localPath, err := filepath.Abs(localPath)
	if err != nil {
		return c, err
	}
	if err := privateTransferDirectory(root); err != nil {
		return c, err
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return c, err
	}
	c = TransferCheckpoint{Version: 1, ID: hex.EncodeToString(id[:]), Direction: direction, Kind: RemotePathFile, LocalPath: localPath, RemotePath: remotePath, Peer: peer, Wire: wire, Phase: "preparing", PrefixSHA256: emptyTransferDigest()}
	dir := filepath.Join(root, c.ID)
	if err := os.Mkdir(dir, 0700); err != nil {
		return c, err
	}
	// Only this freshly created random child is removed on setup failure.
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	if direction == "send" {
		info, err := os.Lstat(localPath)
		if err != nil {
			return c, err
		}
		f, err := os.OpenFile(filepath.Join(dir, "payload"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return c, err
		}
		writer := &transferContextWriter{ctx: ctx, writer: f}
		if info.IsDir() {
			c.Kind = RemotePathDirectory
			var plan directoryPlan
			plan, err = makeDirectoryPlan(localPath)
			if err == nil {
				err = plan.writeTo(writer)
			}
		} else if info.Mode().IsRegular() {
			var source *os.File
			source, err = os.Open(localPath)
			if err == nil {
				_, err = io.Copy(writer, source)
				err = errors.Join(err, source.Close())
			}
		} else {
			err = ErrUnsupportedDirectoryEntry
		}
		if err == nil {
			err = f.Sync()
		}
		err = errors.Join(err, f.Close())
		if err != nil {
			return c, err
		}
		c.Size, c.SHA256, err = hashTransferFile(ctx, filepath.Join(dir, "payload"), -1)
		if err != nil {
			return c, err
		}
	}
	if err := saveTransferCheckpoint(dir, c); err != nil {
		return c, err
	}
	keep = true
	return c, nil
}

// ReadTransferCheckpoint loads a bounded, validated record without acquiring
// device authority. Mutating execution separately holds an OS-backed job lock.
func ReadTransferCheckpoint(root, id string) (TransferCheckpoint, error) {
	var c TransferCheckpoint
	if !checkpointID.MatchString(id) {
		return c, errors.New("invalid transfer checkpoint ID")
	}
	dir := filepath.Join(root, id)
	for _, p := range []string{root, dir} {
		info, err := os.Lstat(p)
		if err != nil {
			return c, err
		}
		if !info.IsDir() {
			return c, errors.New("checkpoint directory is not a real directory")
		}
	}
	p := filepath.Join(dir, "checkpoint.json")
	info, err := os.Lstat(p)
	if err != nil {
		return c, err
	}
	if !info.Mode().IsRegular() || info.Size() > 32768 {
		return c, errors.New("invalid checkpoint file")
	}
	f, err := os.Open(p)
	if err != nil {
		return c, err
	}
	data, err := io.ReadAll(io.LimitReader(f, 32769))
	err = errors.Join(err, f.Close())
	if err != nil {
		return c, err
	}
	if len(data) > 32768 {
		return c, errors.New("checkpoint file exceeds size limit")
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	if c.Version != 1 || c.ID != id || (c.Direction != "send" && c.Direction != "receive") ||
		(c.Kind != RemotePathFile && c.Kind != RemotePathDirectory) || !filepath.IsAbs(c.LocalPath) ||
		len(c.LocalPath) > 4096 || len(c.Peer) > 8192 || c.RemotePath == "" || c.RemotePath[0] != '/' ||
		(c.Wire != FileTransferWireRaw && c.Wire != FileTransferWireBase64) || c.Size < 0 || c.Offset < 0 || c.Offset > c.Size ||
		(c.Offset != c.Size && c.Offset%FileTransferChunkSize != 0) || !checkpointDigest.MatchString(c.PrefixSHA256) {
		return c, errors.New("invalid transfer checkpoint")
	}
	if _, err := quoteRemotePath(c.RemotePath); err != nil {
		return c, err
	}
	switch c.Phase {
	case "preparing":
		if c.Offset != 0 {
			return c, errors.New("invalid preparing checkpoint")
		}
	case "transferring", "publishing", "completed":
		if !checkpointDigest.MatchString(c.SHA256) {
			return c, errors.New("invalid checkpoint digest")
		}
	default:
		return c, errors.New("invalid checkpoint phase")
	}
	return c, nil
}

// ListTransferCheckpoints returns at most 128 incomplete jobs. Corrupt records
// fail closed rather than becoming path or process capabilities in a client.
func ListTransferCheckpoints(root string) ([]TransferCheckpoint, error) {
	result := []TransferCheckpoint{}
	directory, err := os.Open(root)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	for {
		entries, readErr := directory.ReadDir(128)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		for _, entry := range entries {
			if !entry.IsDir() || !checkpointID.MatchString(entry.Name()) {
				continue
			}
			c, err := ReadTransferCheckpoint(root, entry.Name())
			// Snapshot creation publishes JSON only when it is complete. A running
			// or killed pre-checkpoint copy must not hide other recoverable jobs.
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("read transfer %s: %w", entry.Name(), err)
			}
			if c.Phase != "completed" {
				result = append(result, c)
			}
			if len(result) == 128 {
				return result, nil
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	return result, nil
}

func privateTransferDirectory(root string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("transfer storage must be a real directory")
	}
	return nil
}

func lockTransferCheckpoint(root, id string) (*flock.Flock, error) {
	if _, err := ReadTransferCheckpoint(root, id); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(root, id, "lock")
	if info, err := os.Lstat(lockPath); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("invalid checkpoint lock")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	lock := flock.New(lockPath)
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		_ = lock.Close()
		return nil, errors.New("transfer checkpoint is already in use")
	}
	return lock, nil
}

func saveTransferCheckpoint(dir string, c TransferCheckpoint) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".checkpoint-*")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "checkpoint.json"))
}

func emptyTransferDigest() string { sum := sha256.Sum256(nil); return hex.EncodeToString(sum[:]) }

// hashTransferFile rejects symlinks and hashes exactly limit bytes, or the whole
// regular file for -1. Read-only preflight runs before any remote append.
func hashTransferFile(ctx context.Context, path string, limit int64) (int64, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, "", err
	}
	if !info.Mode().IsRegular() {
		return 0, "", errors.New("transfer payload is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	if limit < 0 {
		limit = info.Size()
	}
	h := sha256.New()
	n, err := io.CopyN(&transferContextWriter{ctx: ctx, writer: h}, f, limit)
	return n, hex.EncodeToString(h.Sum(nil)), err
}

type transferContextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w *transferContextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(p)
}
