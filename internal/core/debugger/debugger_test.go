package debugger

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestMIParserNestedAndBinary(t *testing.T) {
	r, e := Parse(`42^done,stack=[frame={level="0",args=[{name="a",value="3"}]}],value="\377\000\n",same="one",same="two"`)
	if e != nil {
		t.Fatal(e)
	}
	if r.Token != "42" || r.Class != "done" || r.Results["value"] != "\xff\x00\n" {
		t.Fatalf("record: %#v", r)
	}
	if !reflect.DeepEqual(r.Results["same"], []any{"one", "two"}) {
		t.Fatal(r.Results)
	}
	for _, input := range []string{`^done,x={`, `^done,x="\q"`, `1`, `^done,x=[v="a",]`, `~"unterminated`, `^done,x="\777"`} {
		if _, e := Parse(input); e == nil {
			t.Fatalf("accepted malformed %q", input)
		}
	}
	if r, e := Parse(`*stopped,reason="exited-normally"`); e != nil || r.Kind != "*" {
		t.Fatalf("async: %v %v", r, e)
	}
}

type relayChunk struct {
	data    []byte
	dropped bool
	err     error
}
type relayStream struct {
	reads  chan relayChunk
	writes chan []byte
}

func (s *relayStream) Read(ctx context.Context) ([]byte, bool, error) {
	select {
	case c := <-s.reads:
		return c.data, c.dropped, c.err
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}
func (s *relayStream) Write(ctx context.Context, p []byte) error {
	select {
	case s.writes <- append([]byte(nil), p...):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestRelayBinaryFragmentsEmptyReadsAndFinalACK(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s := &relayStream{reads: make(chan relayChunk, 16), writes: make(chan []byte, 16)}
	done := make(chan error, 1)
	go func() { done <- Relay(ctx, left, s) }()
	s.reads <- relayChunk{}
	payload := []byte{'$', 'X', 0, 0xff, '}', 3, '#', '0', '0'}
	for _, b := range payload {
		s.reads <- relayChunk{data: []byte{b}}
	}
	received := make([]byte, len(payload))
	if _, e := io.ReadFull(right, received); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(payload, received) {
		t.Fatalf("binary changed: %x", received)
	}
	if _, e := right.Write([]byte("+")); e != nil {
		t.Fatal(e)
	}
	if got := <-s.writes; string(got) != "+" {
		t.Fatalf("last ACK lost: %x", got)
	}
	if _, e := right.Write([]byte{'$', 0xff, '#', 'f', 'f'}); e != nil {
		t.Fatal(e)
	}
	if got := <-s.writes; !bytes.Equal(got, []byte{'$', 0xff, '#', 'f', 'f'}) {
		t.Fatalf("write changed: %x", got)
	}
	right.Close()
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
func TestRelayRefusesDroppedBytes(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	s := &relayStream{reads: make(chan relayChunk, 1), writes: make(chan []byte, 1)}
	s.reads <- relayChunk{data: []byte("must not forward"), dropped: true}
	e := Relay(context.Background(), left, s)
	if !errors.Is(e, ErrDataLost) {
		t.Fatalf("loss: %v", e)
	}
	b := make([]byte, 1)
	if n, _ := right.Read(b); n != 0 {
		t.Fatal("forwarded after loss")
	}
}
func TestRelayDisconnectFails(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	s := &relayStream{reads: make(chan relayChunk, 1), writes: make(chan []byte, 1)}
	cause := errors.New("channel disconnected")
	s.reads <- relayChunk{err: cause}
	if e := Relay(context.Background(), left, s); !errors.Is(e, cause) {
		t.Fatalf("disconnect: %v", e)
	}
}

func TestFinalExitACKTracksFragmentsAndRejectsPayloadACK(t *testing.T) {
	for _, tc := range []struct {
		name, packet string
		ack          bool
	}{{"exit", "$W00#b7", true}, {"signal", "$X09#c1", true}, {"bad checksum", "$W00#00", false}, {"ordinary", "$OK#9a", false}} {
		t.Run(tc.name, func(t *testing.T) {
			tracker := &exitACKTracker{}
			for _, b := range []byte(tc.packet) {
				tracker.target([]byte{b})
			}
			tracker.client([]byte("$X1:+#00"))
			if tracker.acknowledged() {
				t.Fatal("payload plus mistaken for ACK")
			}
			tracker.client([]byte("-"))
			if tracker.acknowledged() {
				t.Fatal("NAK mistaken for ACK")
			}
			tracker.client([]byte("+"))
			if tracker.acknowledged() != tc.ack {
				t.Fatalf("final ACK=%t", tracker.acknowledged())
			}
		})
	}
}

func TestMIQuotePreservesUTF8AndBinaryBytes(t *testing.T) {
	for _, s := range []string{"path with spaces", "调试/加法", "\xff\x00\n\"\\"} {
		r, e := Parse("^done,value=" + Quote(s))
		if e != nil || r.Results["value"] != s {
			t.Fatalf("quote %q: %v %v", s, r, e)
		}
	}
}
