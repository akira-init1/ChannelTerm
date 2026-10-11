package terminalinput

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// TestPromptLineSecretAndEditing verifies echo suppression, Unicode backspace,
// and preservation of input belonging to subsequent operations.
func TestPromptLineSecretAndEditing(t *testing.T) {
	for _, secret := range []bool{false, true} {
		input := strings.NewReader("ab中\x7fc\rnext")
		var output bytes.Buffer
		value, err := readPromptLine(context.Background(), input.ReadByte, &output, secret)
		if err != nil || string(value) != "abc" {
			t.Fatalf("value=%q err=%v", value, err)
		}
		if secret && output.Len() != 0 {
			t.Fatal("password echoed")
		}
		remaining, _ := io.ReadAll(input)
		if string(remaining) != "next" {
			t.Fatal("prompt consumed subsequent input")
		}
	}
}

// TestPromptLineCancellationAndEOF ensures abort paths never return partial secrets.
func TestPromptLineCancellationAndEOF(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  error
	}{
		{"secret\x03", context.Canceled}, {"secret\x04", io.EOF}, {"secret\x1a", io.EOF}, {"secret", io.EOF},
	} {
		value, err := readPromptLine(context.Background(), strings.NewReader(tc.input).ReadByte, io.Discard, true)
		if value != nil || !errors.Is(err, tc.want) {
			t.Fatalf("value=%q err=%v", value, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readPromptLine(ctx, func() (byte, error) { t.Fatal("read after cancellation"); return 0, nil }, io.Discard, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// TestReadLineRejectsPipes prevents accidentally treating scripted input as a password.
func TestReadLineRejectsPipes(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	var output bytes.Buffer
	if _, err := ReadLine(context.Background(), r, &output, "password: ", true); err == nil {
		t.Fatal("pipe accepted for password input")
	}
	if output.Len() != 0 {
		t.Fatal("prompt written before terminal validation")
	}
}
