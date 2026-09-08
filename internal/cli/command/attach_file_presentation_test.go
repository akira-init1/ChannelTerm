package command

import (
	"bytes"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestFileTransferPresentationSuppressesRawTransferDataAndRestoresOutput(t *testing.T) {
	presentation := newFileTransferPresentation()
	var output bytes.Buffer
	write := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	writeRaw := func(data []byte) {
		rendered, err := writeAttachedTerminalOutput(presentation, write, data)
		if err != nil {
			t.Fatalf("writeAttachedTerminalOutput() error = %v", err)
		}
		if !rendered {
			t.Logf("suppressed raw data of %d bytes", len(data))
		}
	}

	writeRaw([]byte("root@board:~# ls\r\n"))
	if err := presentation.handle(session.Event{Type: session.EventFileTransferStarted, Metadata: map[string]any{
		"direction": "send", "local_path": "system.dts", "remote_path": "/tmp/system.dts",
	}}, false, write); err != nil {
		t.Fatalf("handle(started) error = %v", err)
	}
	writeRaw([]byte("t='saved'; stty raw -echo; dd of=/tmp/system.dts\r\n"))
	writeRaw([]byte("@CTERM:token:READY:32768\r\n"))
	writeRaw([]byte{0x00, 0xff, 0x80, 0x01, 0x02})
	if err := presentation.handle(session.Event{Type: session.EventFileTransferProgress, Metadata: map[string]any{"percent": 50.0}}, false, write); err != nil {
		t.Fatalf("handle(progress) error = %v", err)
	}
	if err := presentation.handle(session.Event{Type: session.EventFileTransferCompleted}, false, write); err != nil {
		t.Fatalf("handle(completed) error = %v", err)
	}
	writeRaw([]byte("root@board:~# \r\n"))

	got := output.String()
	for _, hidden := range []string{"stty raw", "dd of=", "@CTERM", string([]byte{0x00, 0xff, 0x80, 0x01, 0x02})} {
		if bytes.Contains(output.Bytes(), []byte(hidden)) {
			t.Errorf("output unexpectedly contains suppressed transfer data %q: %q", hidden, got)
		}
	}
	for _, wanted := range []string{
		"root@board:~# ls", "File transfer started: system.dts -> /tmp/system.dts", "File transfer: 50.0%", "File transfer completed", "root@board:~# ",
	} {
		if !bytes.Contains(output.Bytes(), []byte(wanted)) {
			t.Errorf("output = %q, want %q", got, wanted)
		}
	}
}

func TestFileTransferPresentationFailedTransferRestoresOutputWithoutReplay(t *testing.T) {
	presentation := newFileTransferPresentation()
	var output bytes.Buffer
	write := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	if err := presentation.handle(session.Event{Type: session.EventFileTransferStarted}, false, write); err != nil {
		t.Fatal(err)
	}
	if rendered, err := writeAttachedTerminalOutput(presentation, write, []byte("@CTERM:token:DATA\x00\xff")); err != nil || rendered {
		t.Fatalf("transfer raw = rendered=%t, err=%v; want suppressed", rendered, err)
	}
	if err := presentation.handle(session.Event{Type: session.EventFileTransferFailed, Metadata: map[string]any{"error": "context canceled"}}, false, write); err != nil {
		t.Fatal(err)
	}
	if rendered, err := writeAttachedTerminalOutput(presentation, write, []byte("root@board:~# ")); err != nil || !rendered {
		t.Fatalf("normal output after failure = rendered=%t, err=%v; want rendered", rendered, err)
	}
	if got := output.String(); bytes.Contains(output.Bytes(), []byte("@CTERM")) || !bytes.Contains(output.Bytes(), []byte("File transfer failed: context canceled")) || !bytes.Contains(output.Bytes(), []byte("root@board:~# ")) {
		t.Errorf("output = %q, want failure status and restored prompt without replay", got)
	}
}

func TestFileTransferPresentationUsesIndependentGatesForMultipleObservers(t *testing.T) {
	observers := []*fileTransferPresentation{newFileTransferPresentation(), newFileTransferPresentation()}
	for index, presentation := range observers {
		var output bytes.Buffer
		write := func(data []byte) error {
			_, err := output.Write(data)
			return err
		}
		if err := presentation.handle(session.Event{Type: session.EventFileTransferStarted}, false, write); err != nil {
			t.Fatalf("observer %d handle(started) error = %v", index, err)
		}
		if rendered, err := writeAttachedTerminalOutput(presentation, write, []byte("@CTERM:token:ACK\x00")); err != nil || rendered {
			t.Fatalf("observer %d transfer raw = rendered=%t, err=%v; want suppressed", index, rendered, err)
		}
		if err := presentation.handle(session.Event{Type: session.EventFileTransferCompleted}, false, write); err != nil {
			t.Fatalf("observer %d handle(completed) error = %v", index, err)
		}
		if rendered, err := writeAttachedTerminalOutput(presentation, write, []byte("ready\r\n")); err != nil || !rendered {
			t.Fatalf("observer %d normal raw = rendered=%t, err=%v; want rendered", index, rendered, err)
		}
		if got := output.String(); bytes.Contains(output.Bytes(), []byte("@CTERM")) || !bytes.Contains(output.Bytes(), []byte("ready")) {
			t.Errorf("observer %d output = %q, want restored clean output", index, got)
		}
	}
}

func TestFileTransferPresentationDoesNotDuplicateOwnerProgress(t *testing.T) {
	presentation := newFileTransferPresentation()
	var output bytes.Buffer
	write := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	for _, event := range []session.Event{
		{Type: session.EventFileTransferStarted},
		{Type: session.EventFileTransferProgress, Metadata: map[string]any{"percent": 50.0}},
		{Type: session.EventFileTransferCompleted},
	} {
		if err := presentation.handle(event, true, write); err != nil {
			t.Fatal(err)
		}
	}
	if got := output.String(); got != "" {
		t.Errorf("owner event output = %q, want no duplicate local transfer presentation", got)
	}
}
