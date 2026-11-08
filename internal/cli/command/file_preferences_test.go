package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
)

func TestFilePreferencesLocalReadSaveAndExplicitPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	var output bytes.Buffer
	for _, mode := range []string{"ask", "files", "tar"} {
		output.Reset()
		if err := runFileWithDependencies(context.Background(), []string{"preferences", "--config", path, "--directory-mode", mode, "--json"}, &output, fileCommandDependencies{}); err != nil {
			t.Fatal(err)
		}
		if output.String() != "{\"directory_mode\":\""+mode+"\"}\n" {
			t.Fatal(output.String())
		}
		opts, err := parseFileOptions("file send", []string{"--config", path}, io.Discard, func(io.Writer) {})
		want := mode
		if want == "ask" {
			want = "tar"
		}
		if err != nil || opts.directoryMode != want {
			t.Fatalf("%+v %v", opts, err)
		}
		opts, err = parseFileOptions("file send", []string{"--config", path, "--directory-mode", "files"}, io.Discard, func(io.Writer) {})
		if err != nil || opts.directoryMode != "files" {
			t.Fatalf("%+v %v", opts, err)
		}
	}
	before, _ := os.ReadFile(path)
	for _, value := range []string{"", "zip"} {
		if err := runFilePreferences(context.Background(), []string{"--config", path, "--directory-mode", value}, &output); err == nil {
			t.Fatal("accepted invalid mode")
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid choice changed config")
	}
}

func TestDirectoryChoiceCancellationPrecedesSessionAndDeviceAccess(t *testing.T) {
	called := false
	err := runFileSend(context.Background(), []string{t.TempDir(), "/tmp/fake", "--directory-mode", "tar"}, io.Discard, fileCommandDependencies{
		chooseDirectoryMode: func(context.Context) (string, error) { called = true; return "", context.Canceled },
		newAttach: func(context.Context, string, string) (attachSession, error) {
			t.Fatal("cancelled choice attached a Session")
			return nil, nil
		},
	})
	if !called || !errors.Is(err, context.Canceled) {
		t.Fatalf("choice=%t error=%v", called, err)
	}
}

func isolatedTransferPreference(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	// os.UserConfigDir uses HOME on macOS.
	t.Setenv("HOME", dir)
	path, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDirectoryChoiceRememberCancelAndLocalInput(t *testing.T) {
	path := isolatedTransferPreference(t)
	for _, answer := range []byte{'t', 'f', 'F', 0x1b, 0x03} {
		if err := config.Update(path, func(file *config.File) error { file.Preferences.FileTransfer.DirectoryMode = "ask"; return nil }); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		requests := make(chan chan string)
		result := make(chan error, 1)
		go func() {
			mode, err := choosePreferredDirectory(ctx, requests)
			if err == nil && mode != "tar" && mode != "files" {
				err = errors.New("invalid resolved mode")
			}
			result <- err
		}()
		var reply chan string
		select {
		case reply = <-requests:
		case <-ctx.Done():
			t.Fatal("choice not requested")
		}
		attached := &fakeAttachSession{}
		d := newAttachInputDispatcherWithPump(ctx, &attachInputPump{}, attached, func([]byte) error { return nil }, nil, cancel, nil)
		d.mode = attachInputModeDirectoryChoice
		d.directoryAnswer = reply
		d.transferCancellation = newFileTransferCancellation()
		if !d.dispatch([]byte{answer, 'x', '\r'}) {
			t.Fatal("dispatch failed")
		}
		err := <-result
		if (answer == 0x1b || answer == 0x03) != errors.Is(err, context.Canceled) {
			t.Fatalf("answer %d: %v", answer, err)
		}
		if len(attached.writtenData()) != 0 {
			t.Fatal("choice or pasted tail reached device")
		}
		p, err := config.LoadTransferPreferences(path)
		if err != nil {
			t.Fatal(err)
		}
		mode, _ := p.Mode()
		if answer == 'F' && mode != "files" || answer != 'F' && mode != "ask" {
			t.Fatal(mode)
		}
		cancel()
	}
}

func TestDirectoryPromptDisconnectCancelsWorker(t *testing.T) {
	isolatedTransferPreference(t)
	ctx, cancel := context.WithCancel(context.Background())
	requests := make(chan chan string)
	done := make(chan error, 1)
	go func() { _, err := choosePreferredDirectory(ctx, requests); done <- err }()
	<-requests
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker leaked")
	}
	d := &attachInputDispatcher{mode: attachInputModeDirectoryChoice, transferCancellation: newFileTransferCancellation()}
	d.requestTransferCancellation()
	if !d.transferCancellation.Requested() {
		t.Fatal("prompt prevented disconnect cancellation")
	}
}

func TestPreferredShortcutKeepsExistingExplicitChoices(t *testing.T) {
	for key, want := range map[string]string{"s": "tar", "r": "tar", "u": "files", "d": "files", "f": "preferred", "g": "preferred"} {
		var output bytes.Buffer
		d := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, &fakeAttachSession{}, func(b []byte) error { _, err := output.Write(b); return err }, nil, func() {}, nil)
		d.dispatch([]byte("\x1df" + key))
		if d.directoryMode != want || !strings.Contains(output.String(), "path:") {
			t.Fatalf("%s: %s %s", key, d.directoryMode, output.String())
		}
	}
}

func TestDirectoryChoiceWaitsForCancellationConfirmation(t *testing.T) {
	d := &attachInputDispatcher{mode: attachInputModeFileTransferCancelConfirm, directoryAnswer: make(chan string, 1), transferCancellation: newFileTransferCancellation(), writeLocal: func([]byte) error { return nil }}
	d.transferCancellation.BeginConfirmation()
	d.presentDirectoryChoice()
	if d.mode != attachInputModeFileTransferCancelConfirm {
		t.Fatal("directory prompt replaced cancellation")
	}
	d.resolveTransferCancellationConfirmation(false)
	if d.mode != attachInputModeDirectoryChoice {
		t.Fatal("directory prompt did not resume")
	}
	reply := d.directoryAnswer
	d.answerDirectoryChoice('f')
	if <-reply != "f" {
		t.Fatal("wrong answer")
	}
}
