package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"time"
	"unicode/utf8"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

// readSSHFileCredentials accepts secrets over a pipe, never argv, environment,
// progress records or persisted configuration. EOF terminates the one object;
// unknown fields, trailing objects and oversized inputs fail before connecting.
func readSSHFileCredentials(input io.Reader, config *sshtransport.Config) error {
	var credentials struct {
		Password      *string `json:"password"`
		KeyPassphrase *string `json:"key_passphrase"`
	}
	data, err := io.ReadAll(io.LimitReader(input, 65537))
	if err != nil || len(data) > 65536 {
		return errors.New("invalid or oversized SSH credential input")
	}
	defer clear(data)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("SSH credentials must be an object")
	}
	if err := decoder.Decode(&credentials); err != nil {
		return errors.New("invalid SSH credential object")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid SSH credential object")
	}
	for _, value := range []*string{credentials.Password, credentials.KeyPassphrase} {
		if value != nil && len(*value) > 16384 {
			return errors.New("SSH credential exceeds size limit")
		}
	}
	if credentials.Password != nil {
		used := false
		config.Password = func(ctx context.Context) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if used {
				return nil, errors.New("supplied SSH password was rejected")
			}
			used = true
			return []byte(*credentials.Password), nil
		}
	}
	if credentials.KeyPassphrase != nil {
		config.KeyPassphrase = func(ctx context.Context, _ string) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return []byte(*credentials.KeyPassphrase), nil
		}
	}
	return nil
}

// runSSHFilesJSON streams one bounded record at a time, throttling payload
// updates to five per second. Only a successful operation emits completed;
// reaching the payload size alone is not proof of acknowledgement or verification.
func runSSHFilesJSON(ctx context.Context, output io.Writer, r app.SSHFilesRequest, run func(context.Context, app.SSHFilesRequest) (app.SSHFilesResult, error)) error {
	encode := json.NewEncoder(output).Encode
	var name string
	var last time.Time
	r.Progress = func(current string, n, total int64) error {
		if len(current) > 4096 {
			return errors.New("file progress name exceeds size limit")
		}
		now := time.Now()
		if current == name && !last.IsZero() && n != total && now.Sub(last) < 200*time.Millisecond {
			return nil
		}
		name, last = current, now
		return encode(map[string]any{"type": "progress", "name": current, "transferred": n, "total": total})
	}
	r.Verification = func(event app.SSHFileVerification) error {
		if len(event.Name) > 4096 {
			return errors.New("verification name exceeds size limit")
		}
		kind := "verifying"
		if event.SourceSHA256 != "" {
			kind = "verified"
		}
		return encode(map[string]any{"type": kind, "name": event.Name})
	}
	if err := encode(map[string]any{"type": "started"}); err != nil {
		return err
	}
	result, err := run(ctx, r)
	if r.Operation == "ls" || r.Operation == "stat" {
		return writeSSHEntriesJSON(ctx, encode, r.Operation, result, err)
	}
	kind := "completed"
	if err != nil {
		kind = "failed"
		if errors.Is(err, context.Canceled) {
			kind = "cancelled"
		}
	}
	return errors.Join(err, encode(map[string]any{"type": kind, "files": result.Files, "bytes": result.Bytes, "verified_files": result.VerifiedFiles}))
}

// writeSSHEntriesJSON validates the entire read-only result before exposing any
// entries. Bounds apply to machine output; the existing SFTP client enumerates
// a directory before returning. Human listing and transfer records are unchanged.
func writeSSHEntriesJSON(ctx context.Context, encode func(any) error, operation string, result app.SSHFilesResult, runErr error) error {
	err := runErr
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		if len(result.Entries) > 1024 || operation == "stat" && len(result.Entries) != 1 {
			err = errors.New("SFTP listing exceeds entry limit or has invalid stat result")
		}
		nameBytes := 0
		for _, entry := range result.Entries {
			if err != nil {
				break
			}
			nameBytes += len(entry.Name)
			if len(entry.Name) == 0 || len(entry.Name) > 4096 || !utf8.ValidString(entry.Name) || nameBytes > 1024*1024 || entry.Size < 0 {
				err = errors.New("SFTP listing metadata exceeds limits or is invalid")
				break
			}
		}
	}
	if err != nil {
		kind := "failed"
		if errors.Is(err, context.Canceled) {
			kind = "cancelled"
		}
		code := "operation_failed"
		if runErr == nil && ctx.Err() == nil {
			code = "listing_limit"
		}
		return errors.Join(err, encode(map[string]any{"type": kind, "entries": 0, "error_code": code}))
	}
	for _, entry := range result.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		kind := "other"
		switch {
		case entry.Mode&fs.ModeSymlink != 0:
			kind = "symlink"
		case entry.Mode.IsDir():
			kind = "directory"
		case entry.Mode.IsRegular():
			kind = "file"
		}
		if err := encode(map[string]any{"type": "entry", "name": entry.Name, "kind": kind, "size": entry.Size, "mode": entry.Mode.String(), "modified": entry.Modified.UTC().Format(time.RFC3339Nano)}); err != nil {
			return err
		}
	}
	return encode(map[string]any{"type": "completed", "entries": len(result.Entries)})
}
