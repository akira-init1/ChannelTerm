package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

// SSHFilesRequest describes one dedicated SSH file operation. Local/Remote are
// source/destination paths for put/get; Target is the new remote name for rename.
// Recursive enables directory transfer, not recursive deletion. Overwrite must
// be explicitly enabled for replacement. Progress is per file and may cancel by
// returning an error. Passwords are supplied through Config callbacks only.
type SSHFilesRequest struct {
	Config    sshtransport.Config
	Protocol  string
	Operation string
	Local     string
	Remote    string
	Target    string
	Recursive bool
	Overwrite bool
	// SafeReplace stages each SFTP file beside its destination, then renames
	// it after a complete copy. It requires Overwrite and never makes a whole
	// directory transactional. Remote upload requires POSIX rename support.
	SafeReplace bool
	Progress    func(name string, transferred, total int64) error
}

// SSHFilesResult returns remote entries or successfully copied file/byte counts.
// It never contains credentials or terminal Session identifiers.
type SSHFilesResult struct {
	Entries []sshtransport.FileInfo `json:"entries,omitempty"`
	Files   int64                   `json:"files"`
	Bytes   int64                   `json:"bytes"`
}

// RunSSHFiles owns one operation's dedicated connection and local file handles.
// Cancellation interrupts the SSH connection. Direct copies can leave partial
// destinations; SafeReplace instead attempts to remove uncommitted staging
// files. Neither mode is a transactional tree copy. It does not
// acquire a terminal lease or register a Session and is reusable by non-CLI adapters.
func RunSSHFiles(ctx context.Context, request SSHFilesRequest) (result SSHFilesResult, err error) {
	if err := validateSSHFiles(request); err != nil {
		return result, err
	}
	if request.Operation == "put" {
		if err := validateUploadTree(ctx, request.Local, request.Recursive); err != nil {
			return result, err
		}
	}
	transport, err := sshtransport.New(request.Config)
	if err != nil {
		return result, err
	}
	client, err := transport.OpenFiles(ctx, request.Protocol)
	if err != nil {
		return result, err
	}
	defer func() {
		err = errors.Join(err, client.Close())
		if ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
		}
	}()
	return runSSHFiles(ctx, client, request)
}

func validateSSHFiles(r SSHFilesRequest) error {
	if r.SafeReplace && (r.Protocol != "sftp" || !r.Overwrite || r.Operation != "put" && r.Operation != "get") {
		return errors.New("safe replacement requires SFTP put/get and explicit overwrite")
	}
	if r.Protocol != "sftp" && r.Protocol != "scp" {
		return errors.New("protocol must be sftp or scp")
	}
	if r.Remote == "" || strings.ContainsRune(r.Remote, 0) {
		return errors.New("remote path is required and must not contain NUL")
	}
	if r.Protocol == "scp" && r.Operation != "put" && r.Operation != "get" {
		return errors.New("SCP supports put and get; use SFTP for file management")
	}
	switch r.Operation {
	case "put", "get":
		if r.Local == "" {
			return errors.New("local path is required")
		}
	case "rename":
		if r.Target == "" || strings.ContainsRune(r.Target, 0) {
			return errors.New("rename target is required and must not contain NUL")
		}
	case "ls", "stat", "mkdir", "rm", "rmdir":
	default:
		return fmt.Errorf("unknown SSH file operation %q", r.Operation)
	}
	return nil
}

func runSSHFiles(ctx context.Context, client *sshtransport.Files, r SSHFilesRequest) (result SSHFilesResult, err error) {
	switch r.Operation {
	case "ls":
		result.Entries, err = client.List(r.Remote)
	case "stat":
		var entry sshtransport.FileInfo
		entry, err = client.Stat(r.Remote)
		if err == nil {
			result.Entries = []sshtransport.FileInfo{entry}
		}
	case "mkdir":
		err = client.Mkdir(r.Remote)
	case "rename":
		err = client.Rename(r.Remote, r.Target, r.Overwrite)
	case "rm", "rmdir":
		err = client.Remove(r.Remote, r.Operation == "rmdir")
	case "put":
		if r.Protocol == "scp" {
			absolute, absErr := filepath.Abs(r.Local)
			if absErr != nil {
				return result, absErr
			}
			root, openErr := os.OpenRoot(filepath.Dir(absolute))
			if openErr != nil {
				return result, openErr
			}
			defer root.Close()
			result.Files, result.Bytes, err = client.UploadSCP(root.FS(), filepath.Base(absolute), r.Remote, r.Recursive, r.Overwrite, r.Progress)
		} else {
			err = uploadSFTP(ctx, client, r, &result)
		}
	case "get":
		err = downloadSSH(ctx, client, r, &result)
	}
	if err != nil {
		return result, fmt.Errorf("%s %s: %w", r.Protocol, r.Operation, err)
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Name < result.Entries[j].Name })
	return result, nil
}

func validateUploadTree(ctx context.Context, local string, recursive bool) error {
	local, err := filepath.Abs(local)
	if err != nil {
		return err
	}
	info, err := os.Lstat(local)
	if err != nil {
		return err
	}
	if info.IsDir() && !recursive {
		return errors.New("directory transfer requires --recursive")
	}
	return filepath.WalkDir(local, func(current string, entry fs.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		if !sshtransport.ValidFileName(entry.Name()) {
			return fmt.Errorf("unsupported filename %q", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("%q: only regular files and directories may be uploaded", current)
		}
		return nil
	})
}

func copySSHFile(ctx context.Context, dst io.Writer, src io.Reader, name string, size int64, r SSHFilesRequest) (int64, error) {
	if size < 0 {
		return 0, errors.New("invalid negative file size")
	}
	var copied int64
	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		limit := len(buffer)
		if remaining := size - copied; remaining < int64(limit) {
			limit = int(remaining) + 1
		}
		n, readErr := src.Read(buffer[:limit])
		if int64(n) > size-copied {
			return copied, errors.New("file grew during transfer")
		}
		if n > 0 {
			written, err := dst.Write(buffer[:n])
			copied += int64(written)
			if err != nil {
				return copied, err
			}
			if written != n {
				return copied, io.ErrShortWrite
			}
			if r.Progress != nil {
				if err := r.Progress(name, copied, size); err != nil {
					return copied, err
				}
			}
		}
		if readErr == io.EOF {
			if copied != size {
				return copied, fmt.Errorf("file size changed: copied %d, expected %d", copied, size)
			}
			return copied, nil
		}
		if readErr != nil {
			return copied, readErr
		}
		if n == 0 {
			return copied, io.ErrNoProgress
		}
	}
}

func uploadSFTP(ctx context.Context, client *sshtransport.Files, r SSHFilesRequest, result *SSHFilesResult) error {
	if r.SafeReplace {
		if err := client.RequirePOSIXRename(); err != nil {
			return err
		}
	}
	absolute, err := filepath.Abs(r.Local)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(absolute)
	destination := r.Remote
	if info, err := client.Stat(destination); err == nil && info.Mode.IsDir() {
		destination = sshRemoteChild(destination, name)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return fs.WalkDir(root.FS(), name, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return errors.New("upload does not follow symlinks")
		}
		rel := strings.TrimPrefix(current, name)
		target := destination + rel
		info, err := entry.Info()
		if err != nil {
			return err
		}
		existing, statErr := client.Stat(target)
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return statErr
		}
		if statErr == nil {
			if !r.Overwrite {
				return fmt.Errorf("destination exists: %s", target)
			}
			if existing.Mode&fs.ModeSymlink != 0 || existing.Mode.IsDir() != info.IsDir() || !info.IsDir() && !existing.Mode.IsRegular() {
				return errors.New("refusing to replace a symlink or different file type")
			}
		}
		if info.IsDir() {
			if statErr == nil {
				return nil
			}
			return client.Mkdir(target)
		}
		if !info.Mode().IsRegular() {
			return errors.New("unsupported source type")
		}
		source, err := root.Open(current)
		if err != nil {
			return err
		}
		defer source.Close()
		if r.SafeReplace {
			return replaceSFTPUpload(ctx, client, source, target, current, info.Size(), r, result)
		}
		sink, err := client.Create(target, r.Overwrite)
		if err != nil {
			return err
		}
		n, copyErr := copySSHFile(ctx, sink, source, current, info.Size(), r)
		result.Bytes += n
		if err := errors.Join(copyErr, sink.Close()); err != nil {
			return err
		}
		result.Files++
		return nil
	})
}

func downloadSSH(ctx context.Context, client *sshtransport.Files, r SSHFilesRequest, result *SSHFilesResult) error {
	base := path.Base(path.Clean(r.Remote))
	if !sshtransport.ValidFileName(base) {
		return errors.New("remote source must name a portable file or directory")
	}
	destination, err := filepath.Abs(r.Local)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(destination); err == nil && info.IsDir() {
		destination = filepath.Join(destination, base)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer root.Close()
	localBase := filepath.Base(destination)
	seen := make(map[string]bool)
	receive := func(entry sshtransport.SCPEntry, source io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		parts := strings.Split(entry.Path, "/")
		if len(parts) == 0 || parts[0] != base {
			return errors.New("unexpected transfer root")
		}
		for _, name := range parts {
			if !sshtransport.ValidFileName(name) {
				return errors.New("unsafe download name")
			}
		}
		key := strings.ToLower(entry.Path)
		if seen[key] || len(seen) >= 100000 || len(parts) > 128 {
			return errors.New("duplicate download name or tree limit exceeded")
		}
		seen[key] = true
		parts[0] = localBase
		local := filepath.Join(parts...)
		existing, statErr := root.Lstat(local)
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return statErr
		}
		if statErr == nil {
			if !r.Overwrite {
				return fmt.Errorf("local destination exists: %s", local)
			}
			if existing.Mode()&fs.ModeSymlink != 0 || existing.IsDir() != entry.Mode.IsDir() || !existing.IsDir() && !existing.Mode().IsRegular() {
				return errors.New("refusing to replace a local symlink or different file type")
			}
		}
		if entry.Mode.IsDir() {
			if statErr == nil {
				return nil
			}
			return root.Mkdir(local, 0700)
		}
		if !entry.Mode.IsRegular() {
			return errors.New("download supports only regular files and directories")
		}
		if r.SafeReplace {
			return replaceSFTPDownload(ctx, root, source, local, entry.Path, entry.Size, r, result)
		}
		flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
		if r.Overwrite {
			flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		}
		file, err := root.OpenFile(local, flags, 0600)
		if err != nil {
			return err
		}
		n, copyErr := copySSHFile(ctx, file, source, entry.Path, entry.Size, r)
		result.Bytes += n
		if err := errors.Join(copyErr, file.Close()); err != nil {
			return err
		}
		result.Files++
		return nil
	}
	if r.Protocol == "scp" {
		return client.DownloadSCP(r.Remote, r.Recursive, receive)
	}
	var walk func(string, string, int) error
	walk = func(remote, relative string, depth int) error {
		if depth > 128 {
			return errors.New("remote directory depth exceeds 128")
		}
		info, err := client.Stat(remote)
		if err != nil {
			return err
		}
		entry := sshtransport.SCPEntry{Path: relative, Size: info.Size, Mode: info.Mode}
		if info.Mode.IsDir() {
			if !r.Recursive {
				return errors.New("directory transfer requires --recursive")
			}
			if err := receive(entry, nil); err != nil {
				return err
			}
			children, err := client.List(remote)
			if err != nil {
				return err
			}
			for _, child := range children {
				if !sshtransport.ValidFileName(child.Name) {
					return errors.New("unsafe remote directory entry")
				}
				if err := walk(sshRemoteChild(remote, child.Name), path.Join(relative, child.Name), depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		if !info.Mode.IsRegular() {
			return errors.New("SFTP download does not follow symlinks or special files")
		}
		file, err := client.Open(remote)
		if err != nil {
			return err
		}
		defer file.Close()
		return receive(entry, file)
	}
	return walk(r.Remote, base, 0)
}

// sshRemoteChild appends a validated basename without interpreting the remote
// filesystem. Cleaning parent/.. locally changes symlink traversal semantics
// and can redirect transfers away from the caller's literal remote path.
func sshRemoteChild(parent, name string) string {
	return strings.TrimRight(parent, "/") + "/" + name
}
