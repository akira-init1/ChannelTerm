package command

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestFileDirectoryModeValidation(t *testing.T) {
	for _, tt := range []struct {
		args    []string
		want    string
		invalid bool
	}{
		{nil, "tar", false},
		{[]string{"--directory-mode", "files"}, "files", false},
		{[]string{"--directory-mode", "zip"}, "", true},
		{[]string{"--directory-mode", "files", "--resumable"}, "", true},
	} {
		opts, err := parseFileOptions("file send", tt.args, io.Discard, func(io.Writer) {})
		if (err != nil) != tt.invalid || (!tt.invalid && opts.directoryMode != tt.want) {
			t.Fatalf("%v: %+v %v", tt.args, opts, err)
		}
	}
}

func TestAttachPerFileChoicesStayLocal(t *testing.T) {
	for _, choice := range []string{"u", "d"} {
		t.Run(choice, func(t *testing.T) {
			attached := &fakeAttachSession{}
			var output bytes.Buffer
			d := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, attached, func(b []byte) error { _, err := output.Write(b); return err }, func() error { return nil }, func() {}, nil)
			if !d.dispatch([]byte("\x1df" + choice)) {
				t.Fatal("menu exited")
			}
			if d.directoryMode != "files" {
				t.Fatal("per-file mode was not selected")
			}
			if len(attached.writtenData()) != 0 {
				t.Fatal("local choice reached board")
			}
			if !d.dispatch([]byte{0x1b}) || !strings.Contains(output.String(), "File transfer cancelled") {
				t.Fatal("escape did not cancel path input")
			}
			d.enterFileMenu()
			if d.directoryMode != "tar" {
				t.Fatal("next menu inherited previous selection")
			}
		})
	}
	wrapped := nonClosingAttachSession{attachSession: nonClosingAttachSession{attachSession: &fakeAttachSession{}, directoryMode: "files"}}
	if args := shortcutTransferArguments(wrapped, "source", "target"); !reflect.DeepEqual(args, []string{"source", "target", "--session", "attached", "--directory-mode", "files"}) {
		t.Fatal(args)
	}
}

func TestDirectoryFilesMachineSummaryNamesDigestScope(t *testing.T) {
	var output bytes.Buffer
	if err := writeDirectoryFilesVerificationSummary(&fileMachineWriter{output: &output}, "digest", "saved", 3); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"checksum_scope":"manifest"`, `"files_verified":3`, `"type":"verified"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("summary=%s", output.String())
		}
	}
}
