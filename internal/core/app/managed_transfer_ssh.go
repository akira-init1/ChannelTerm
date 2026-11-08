package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

func (r *transferRun) openManagedSSH(ctx context.Context, settings config.FileTransfer) (*sshtransport.Transport, *sshtransport.Files, error) {
	p := settings.SSH[r.request.SSHProfile]
	if !filepath.IsAbs(p.PrivateKey) || !filepath.IsAbs(p.KnownHosts) {
		return nil, nil, errors.New("SSH profile private_key and known_hosts must be absolute Host paths")
	}
	t, err := sshtransport.New(sshtransport.Config{User: p.User, Host: p.Host, Port: p.Port, PrivateKeyPath: p.PrivateKey, KnownHostsPath: p.KnownHosts, Timeout: 10 * time.Second, KeepAliveInterval: 15 * time.Second})
	if err != nil {
		return nil, nil, err
	}
	// Metadata, capacity and tree readback always use SFTP, including explicit
	// SCP payloads. Do not silently fall back to unbounded remote shell commands.
	c, err := t.OpenFiles(ctx, "sftp")
	return t, c, err
}

func managedSSHSpace(client *sshtransport.Files, destination string, footprint transferFootprint) error {
	v, err := client.Storage(destination)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTransferSpaceUnavailable, err)
	}
	return checkManagedSSHStorage(destination, footprint, v)
}

func checkManagedSSHStorage(destination string, footprint transferFootprint, v sshtransport.FileStorage) error {
	if v.BlockSize == 0 || v.BlockSize > math.MaxInt64 || v.AvailableBlocks > math.MaxInt64/v.BlockSize {
		return ErrTransferSpaceUnavailable
	}
	inodes := int64(-1)
	// Zero or unsigned -1 means no inode limit, as on filesystems without inode
	// accounting. Do not reject their otherwise usable byte-capacity report.
	if v.TotalFiles > 0 && v.TotalFiles <= math.MaxInt64 {
		if v.AvailableFiles > math.MaxInt64 {
			return ErrTransferSpaceUnavailable
		}
		inodes = int64(v.AvailableFiles)
	}
	return checkTransferStorage(destination, footprint, transferStorage{available: int64(v.AvailableBlocks * v.BlockSize), block: int64(v.BlockSize), inodes: inodes})
}

// managedSSHManifest hashes through the dedicated SFTP connection. The same
// bounded, portable tree contract is used on all six Host platforms; symlinks,
// devices and ambiguous duplicate names cannot enter the local snapshot.
func managedSSHManifest(ctx context.Context, client *sshtransport.Files, remote string) (directoryManifest, bool, error) {
	info, err := client.Stat(remote)
	if err != nil {
		return nil, false, err
	}
	single := info.Mode.IsRegular()
	var m directoryManifest
	var walk func(string, string, sshtransport.FileInfo) error
	walk = func(remote, relative string, info sshtransport.FileInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(m) >= directoryManifestLimit {
			return errors.New("remote tree exceeds 10000 entries")
		}
		if info.Mode.IsDir() {
			if relative != "" {
				m = append(m, directoryManifestEntry{Name: relative, Kind: "directory"})
			}
			children, err := client.List(remote)
			if err != nil {
				return err
			}
			for _, child := range children {
				if !sshtransport.ValidFileName(child.Name) {
					return ErrUnsafeTarPath
				}
				if err := walk(path.Join(remote, child.Name), path.Join(relative, child.Name), child); err != nil {
					return err
				}
			}
			return nil
		}
		if !info.Mode.IsRegular() {
			return ErrUnsupportedDirectoryEntry
		}
		digest, err := hashSSHFile(ctx, info.Size, func() (io.ReadCloser, error) { return client.Open(remote) })
		if err != nil {
			return err
		}
		m = append(m, directoryManifestEntry{Name: relative, Kind: "file", Size: info.Size, SHA256: digest})
		return nil
	}
	name := ""
	if single {
		name = path.Base(remote)
	}
	if err := walk(remote, name, info); err != nil {
		return nil, false, err
	}
	if err := m.validate(); err != nil {
		return nil, false, err
	}
	return m, single, nil
}

func (r *transferRun) runManagedSSH(ctx context.Context, transport *sshtransport.Transport, metadata *sshtransport.Files, q SSHFilesRequest) (SSHFilesResult, error) {
	q.Protocol, q.Verify = r.request.Transport, "sha256"
	q.Progress = func(name string, n, total int64) error { return r.progress(name)(n, total) }
	q.Verification = func(v SSHFileVerification) error {
		if v.DestinationSHA256 != "" {
			r.update(func(s *TransferStatus) { s.FilesVerified++; s.Bytes += s.CurrentTotal })
		}
		return ctx.Err()
	}
	if q.Protocol == "sftp" {
		return runSSHFiles(ctx, metadata, q)
	}
	client, err := transport.OpenFiles(ctx, "scp")
	if err != nil {
		return SSHFilesResult{}, err
	}
	result, err := runSSHFiles(ctx, client, q)
	return result, errors.Join(err, client.Close())
}

func (r *transferRun) sendSSH(ctx context.Context, payload string, single bool, settings config.FileTransfer) (err error) {
	t, client, err := r.openManagedSSH(ctx, settings)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, client.Close()) }()
	destination := r.request.Destination
	if _, err := client.Stat(destination); !errors.Is(err, fs.ErrNotExist) {
		if err == nil {
			err = errors.New("remote destination already exists; choose another path")
		}
		return err
	}
	var expected directoryManifest
	if single {
		n, digest, err := hashTransferFile(ctx, payload, -1)
		if err != nil {
			return err
		}
		expected = directoryManifest{{Name: path.Base(destination), Kind: "file", Size: n, SHA256: digest}}
	} else {
		expected, err = localDirectoryManifest(ctx, payload)
		if err != nil {
			return err
		}
	}
	if err := managedSSHSpace(client, destination, expected.footprint()); err != nil {
		return err
	}
	r.update(func(s *TransferStatus) {
		s.WireMode, s.ResolvedPath = r.request.Transport, destination
		s.FilesTotal = expected.result("").Files
	})
	result, err := r.runManagedSSH(ctx, t, client, SSHFilesRequest{Operation: "put", Local: payload, Remote: destination, Recursive: !single})
	if err != nil {
		return err
	}
	actual, _, err := managedSSHManifest(ctx, client, destination)
	if err != nil {
		return err
	}
	if err := compareDirectoryManifests(expected, actual); err != nil {
		return err
	}
	verified := expected.result(destination)
	r.update(func(s *TransferStatus) {
		s.Bytes, s.FilesVerified, s.SHA256, s.ChecksumScope = result.Bytes, int(result.VerifiedFiles), verified.SHA256, "manifest"
		s.TotalBytes = result.Bytes
		if single {
			s.SHA256, s.ChecksumScope = expected[0].SHA256, "file"
		}
		if r.request.Mode == "tar" || r.request.Mode == "tar.gz" {
			s.ChecksumScope = "archive"
		}
	})
	return nil
}

func (r *transferRun) receiveSSH(ctx context.Context, tree string, settings config.FileTransfer) (single bool, err error) {
	t, client, err := r.openManagedSSH(ctx, settings)
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, client.Close()) }()
	type source struct {
		remote   string
		single   bool
		manifest directoryManifest
	}
	var sources []source
	f := transferFootprint{entries: 1}
	for _, remote := range r.request.Sources {
		m, isFile, err := managedSSHManifest(ctx, client, remote)
		if err != nil {
			return false, err
		}
		add := m.footprint()
		if add.bytes > math.MaxInt64-f.bytes {
			return false, ErrTransferSpaceUnavailable
		}
		f.bytes += add.bytes
		f.entries += add.entries
		sources = append(sources, source{remote, isFile, m})
	}
	if err := r.checkReceiveSpace(tree, settings.Root, f); err != nil {
		return false, err
	}
	r.update(func(s *TransferStatus) {
		s.WireMode, s.TotalBytes = r.request.Transport, f.bytes
		for _, source := range sources {
			s.FilesTotal += source.manifest.result("").Files
		}
	})
	single = len(sources) == 1 && sources[0].single
	for _, src := range sources {
		local := filepath.Join(tree, path.Base(src.remote))
		_, err := r.runManagedSSH(ctx, t, client, SSHFilesRequest{Operation: "get", Local: local, Remote: src.remote, Recursive: !src.single})
		if err != nil {
			return false, err
		}
		var got directoryManifest
		if src.single {
			n, digest, err := hashTransferFile(ctx, local, -1)
			if err != nil {
				return false, err
			}
			got = directoryManifest{{Name: path.Base(src.remote), Kind: "file", Size: n, SHA256: digest}}
		} else {
			got, err = localDirectoryManifest(ctx, local)
			if err != nil {
				return false, err
			}
		}
		if err := compareDirectoryManifests(src.manifest, got); err != nil {
			return false, err
		}
		current, _, err := managedSSHManifest(ctx, client, src.remote)
		if err != nil {
			return false, err
		}
		if err := compareDirectoryManifests(src.manifest, current); err != nil {
			return false, err
		}
		if !src.single && len(sources) == 1 {
			// Both paths are in private staging. Move the received root out before
			// replacing the empty wrapper, preserving single-directory semantics.
			sibling := filepath.Join(filepath.Dir(tree), "received")
			if err := os.Rename(local, sibling); err != nil {
				return false, err
			}
			if err := os.Remove(tree); err != nil {
				return false, err
			}
			if err := os.Rename(sibling, tree); err != nil {
				return false, err
			}
		}
	}
	return single, nil
}
