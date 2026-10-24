package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func testTerminalCodec(t *testing.T, name, view string) *terminalCodec {
	t.Helper()
	c, err := (&terminalPreferences{encoding: name, view: view}).codec()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTerminalEncodingSplitCharacters(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		wire       []byte
	}{
		{"gbk", "中文", []byte{0xd6, 0xd0, 0xce, 0xc4}},
		{"gb18030", "😀", []byte{0x94, 0x39, 0xfc, 0x36}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for split := 0; split <= len(tc.wire); split++ {
				c := testTerminalCodec(t, tc.name, "text")
				a, err := c.output(tc.wire[:split], false)
				if err != nil {
					t.Fatal(err)
				}
				b, err := c.output(tc.wire[split:], true)
				if err != nil {
					t.Fatal(err)
				}
				if string(append(a, b...)) != tc.text {
					t.Fatalf("split %d: %q %q", split, a, b)
				}
			}
			for split := 0; split <= len(tc.text); split++ {
				c := testTerminalCodec(t, tc.name, "text")
				a, err := c.input([]byte(tc.text[:split]))
				if err != nil {
					t.Fatal(err)
				}
				b, err := c.input([]byte(tc.text[split:]))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(append(a, b...), tc.wire) {
					t.Fatalf("input split %d: %x %x", split, a, b)
				}
			}
		})
	}
}

func TestTerminalEncodingRejectsLossyInputAndResetsAfterGap(t *testing.T) {
	c := testTerminalCodec(t, "gbk", "text")
	for _, input := range [][]byte{[]byte("hello😀"), {0xff}} {
		if data, err := c.input(input); err == nil || len(data) != 0 {
			t.Fatalf("accepted invalid input: %x, %v", data, err)
		}
	}
	if data, err := c.input([]byte("ok")); err != nil || string(data) != "ok" {
		t.Fatalf("recovery: %q %v", data, err)
	}
	if _, err := c.output([]byte{0xd6}, false); err != nil {
		t.Fatal(err)
	}
	c.resetOutput()
	if data, err := c.output([]byte("ok"), true); err != nil || string(data) != "ok" {
		t.Fatalf("gap recovery: %q %v", data, err)
	}
}

func TestTerminalUTF8PreservesRawDefault(t *testing.T) {
	c := testTerminalCodec(t, "UTF8", "text")
	wire := []byte{0xff, 0x1b, '[', 'm', 0xe4}
	output, err := c.output(wire, true)
	if err != nil || !bytes.Equal(output, wire) {
		t.Fatalf("output: %x %v", output, err)
	}
	input, err := c.input(wire)
	if err != nil || !bytes.Equal(input, wire) {
		t.Fatalf("input: %x %v", input, err)
	}
}

func TestTerminalHexIsReadOnlyAndDoesNotExecuteEscapes(t *testing.T) {
	c := testTerminalCodec(t, "utf-8", "hex")
	out, err := c.output([]byte{0x1b, '[', '2', 'J', 0, 0xff}, false)
	if err != nil || bytes.ContainsRune(out, 0x1b) || !strings.Contains(string(out), "1b 5b 32 4a 00 ff") || !strings.Contains(string(out), "|.[2J..|") {
		t.Fatalf("hex: %q %v", out, err)
	}
	out, _ = c.output([]byte{'a'}, false)
	if !strings.HasPrefix(string(out), "00000006") {
		t.Fatalf("offset: %q", out)
	}
	target := &fakeAttachSession{}
	writer := encodedTerminalWriter{target: target, codec: c}
	if _, err := writer.Write(session.WriteRequest{Data: []byte("unsafe"), Actor: session.ActorUser}); err == nil || len(target.writtenData()) != 0 {
		t.Fatal("hex view wrote to device")
	}
}

func TestTerminalSettingsValidatedBeforeAttach(t *testing.T) {
	for _, args := range [][]string{{"--encoding", "unknown"}, {"--view", "binary"}, {"--description", "bad\x1b[2J"}, {"--description", strings.Repeat("a", 513)}} {
		err := runAttachSession(context.Background(), append(args, "SER-1"), strings.NewReader(""), io.Discard, func(context.Context, string, string) (attachSession, error) {
			t.Fatal("attached before validation")
			return nil, nil
		})
		if err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}

func TestTerminalClientFlagsDoNotReachSerialSetup(t *testing.T) {
	args := []string{"--encoding", "gbk", "--view=hex", "--description", "board", "--baud", "9600"}
	if got := stripAttachControlFlags(args); !reflect.DeepEqual(got, []string{"--baud", "9600"}) {
		t.Fatalf("serial flags: %q", got)
	}
}

type failingEncodedWriter struct{ calls int }

func (w *failingEncodedWriter) Write(session.WriteRequest) (int, error) {
	w.calls++
	return 0, errors.New("rejected")
}

func TestTerminalRejectedWriteIsNotReplayed(t *testing.T) {
	c := testTerminalCodec(t, "gbk", "text")
	target := &failingEncodedWriter{}
	w := encodedTerminalWriter{target: target, codec: c}
	if _, err := w.Write(session.WriteRequest{Data: append([]byte("a"), 0xe4)}); err == nil {
		t.Fatal("expected rejection")
	}
	if len(c.inputTail) != 0 || target.calls != 1 {
		t.Fatal("rejected input retained")
	}
}
