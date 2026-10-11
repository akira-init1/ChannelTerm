package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"sync"
	"time"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// FileInfo describes a remote entry without exposing SSH/SFTP backend types.
type FileInfo struct {
	Name     string      `json:"name"`
	Size     int64       `json:"size"`
	Mode     fs.FileMode `json:"mode"`
	Modified time.Time   `json:"modified"`
}

func fileInfo(info fs.FileInfo) FileInfo {
	return FileInfo{Name: info.Name(), Size: info.Size(), Mode: info.Mode(), Modified: info.ModTime()}
}

// Files owns a dedicated SSH connection for file operations, never a PTY or
// terminal Session. Operations are caller-serialized. Close may run concurrently
// to interrupt blocked I/O; cancellation of the opening context also closes the
// connection. Call Close even after cancellation to join owned client workers.
type Files struct {
	client *gossh.Client
	sftp   *sftp.Client
	ctx    context.Context
	stop   func() bool
	done   chan struct{}
	once   sync.Once
	err    error
}

// OpenFiles authenticates a dedicated file connection. protocol is "sftp" or
// "scp". SFTP requests a subsystem; SCP opens an exec channel per transfer.
// Config.Timeout bounds setup while ctx bounds the complete file-client lifetime.
// The returned connection is not registered with the shared Session Manager.
func (t *Transport) OpenFiles(ctx context.Context, protocol string) (*Files, error) {
	if protocol != "sftp" && protocol != "scp" {
		return nil, errors.New("file protocol must be sftp or scp")
	}
	f := &Files{ctx: ctx, done: make(chan struct{})}
	client, err := t.open(ctx, func(client *gossh.Client) error {
		if protocol == "scp" {
			return nil
		}
		var err error
		f.sftp, err = sftp.NewClient(client)
		if err != nil {
			return fmt.Errorf("start SFTP subsystem: %w", err)
		}
		return nil
	})
	if err != nil {
		if f.sftp != nil {
			_ = f.sftp.Close()
		}
		return nil, err
	}
	f.client = client
	f.stop = context.AfterFunc(ctx, func() { _ = client.Close(); close(f.done) })
	if ctx.Err() != nil {
		_ = f.Close()
		return nil, ctx.Err()
	}
	return f, nil
}

// Close interrupts active file operations and releases the dedicated connection.
// It is idempotent and safe to call concurrently with cancellation or I/O.
func (f *Files) Close() error {
	f.once.Do(func() {
		f.err = f.client.Close()
		if !f.stop() {
			<-f.done
		}
		if f.sftp != nil {
			_ = f.sftp.Close()
		}
		if errors.Is(f.err, net.ErrClosed) || errors.Is(f.err, io.EOF) {
			f.err = nil
		}
	})
	return f.err
}

func (f *Files) requireSFTP() error {
	if err := f.ctx.Err(); err != nil {
		return err
	}
	if f.sftp == nil {
		return errors.New("operation requires SFTP")
	}
	return nil
}

// List returns the immediate children of a remote directory, including symlinks.
func (f *Files) List(path string) ([]FileInfo, error) {
	if err := f.requireSFTP(); err != nil {
		return nil, err
	}
	infos, err := f.sftp.ReadDirContext(f.ctx, path)
	if err != nil {
		return nil, err
	}
	result := make([]FileInfo, 0, len(infos))
	for _, info := range infos {
		result = append(result, fileInfo(info))
	}
	return result, nil
}

// Stat reports an entry itself, without following its final symlink.
func (f *Files) Stat(path string) (FileInfo, error) {
	if err := f.requireSFTP(); err != nil {
		return FileInfo{}, err
	}
	info, err := f.sftp.Lstat(path)
	if err != nil {
		return FileInfo{}, err
	}
	return fileInfo(info), nil
}

// Mkdir creates exactly one remote directory; its parent must already exist.
func (f *Files) Mkdir(path string) error {
	if err := f.requireSFTP(); err != nil {
		return err
	}
	return f.sftp.Mkdir(path)
}

// Rename moves an entry. Replacement requires the server's POSIX rename
// extension; no delete-then-rename fallback risks destroying the destination.
func (f *Files) Rename(oldPath, newPath string, overwrite bool) error {
	if err := f.requireSFTP(); err != nil {
		return err
	}
	if overwrite {
		return f.sftp.PosixRename(oldPath, newPath)
	}
	return f.sftp.Rename(oldPath, newPath)
}

// Remove removes a file/symlink, or an empty directory when directory is true.
// Recursive deletion is deliberately not implicit in either operation.
func (f *Files) Remove(path string, directory bool) error {
	if err := f.requireSFTP(); err != nil {
		return err
	}
	if directory {
		return f.sftp.RemoveDirectory(path)
	}
	info, err := f.sftp.Lstat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("rm requires a file or symlink; use rmdir for an empty directory")
	}
	return f.sftp.Remove(path)
}

// Open reads a remote file. The caller must close the handle before Files.Close.
func (f *Files) Open(path string) (io.ReadCloser, error) {
	if err := f.requireSFTP(); err != nil {
		return nil, err
	}
	return f.sftp.Open(path)
}

// Create creates a remote regular file, rejecting an existing path unless
// overwrite is explicitly requested. A failed transfer may leave partial bytes.
func (f *Files) Create(path string, overwrite bool) (io.WriteCloser, error) {
	if err := f.requireSFTP(); err != nil {
		return nil, err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if overwrite {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	return f.sftp.OpenFile(path, flags)
}
