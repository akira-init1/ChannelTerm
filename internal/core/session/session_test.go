package session

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
)

func TestCoreConnectAndOutput(t *testing.T) {
	terminal := newFakeTransport()
	s, err := New("board-1", terminal, WithReceiveBufferCapacity(64))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	terminal.emit([]byte("ready"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	chunk, err := s.ReadOutput(ctx, 0, 64)
	if err != nil {
		t.Fatalf("ReadOutput() error = %v", err)
	}
	if got := string(chunk.Data); got != "ready" {
		t.Errorf("ReadOutput() = %q, want %q", got, "ready")
	}
	if chunk.Next != 5 {
		t.Errorf("chunk.Next = %d, want 5", chunk.Next)
	}
	if _, err := s.Write(WriteRequest{Actor: ActorSystem, Data: []byte("status\n")}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := string(terminal.writtenData()); got != "status\n" {
		t.Errorf("written = %q, want %q", got, "status\n")
	}
	if err := s.Resize(120, 40); err != nil {
		t.Fatalf("Resize() error = %v", err)
	}
	if cols, rows := terminal.dimensions(); cols != 120 || rows != 40 {
		t.Errorf("Resize() = (%d, %d), want (120, 40)", cols, rows)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestCoreWriteRejectsInvalidActorWithoutWriting(t *testing.T) {
	terminal := newFakeTransport()
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer s.Close()

	if _, err := s.Write(WriteRequest{Actor: "unknown", Data: []byte("status\n")}); !errors.Is(err, ErrInvalidActor) {
		t.Errorf("Write() error = %v, want ErrInvalidActor", err)
	}
	if got := terminal.writtenData(); len(got) != 0 {
		t.Errorf("invalid actor wrote %q, want no transport data", got)
	}
}

func TestCoreWriteRecordsActivityForEachActorWithoutChangingTerminalOutput(t *testing.T) {
	terminal := newFakeTransport()
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer s.Close()

	requests := []WriteRequest{
		{Actor: ActorUser, Data: []byte("human")},
		{Actor: ActorAgent, Data: []byte{0x00, 0xFF, 'a'}},
		{Actor: ActorSystem, Data: []byte("wake\r")},
	}
	for _, request := range requests {
		if written, writeErr := s.Write(request); writeErr != nil || written != len(request.Data) {
			t.Fatalf("Write(%q) = (%d, %v), want (%d, nil)", request.Actor, written, writeErr, len(request.Data))
		}
	}

	activity, err := s.ReadRecentActivity(3)
	if err != nil {
		t.Fatalf("ReadRecentActivity() error = %v", err)
	}
	if len(activity.Events) != len(requests) || activity.Next != 3 {
		t.Fatalf("activity = %+v, want three events through cursor 3", activity)
	}
	for index, event := range activity.Events {
		if event.Timestamp.IsZero() {
			t.Errorf("event %d has zero timestamp", index)
		}
		if event.Actor != requests[index].Actor || event.Operation != OperationWrite || string(event.Data) != string(requests[index].Data) {
			t.Errorf("event %d = %+v, want actor=%q write data=%x", index, event, requests[index].Actor, requests[index].Data)
		}
	}
	if got := string(terminal.writtenData()); got != "human\x00\xffawake\r" {
		t.Errorf("Transport bytes = %q, want unmodified concatenated request data", got)
	}
	output, err := s.ReadRecent(16)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if len(output.Data) != 0 {
		t.Errorf("terminal output contains Activity data %q, want empty", output.Data)
	}
}

func TestCoreWriteRecordsOnlyPartialTransportPayload(t *testing.T) {
	terminal := &partialWriteTransport{fakeTransport: newFakeTransport()}
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer s.Close()

	written, writeErr := s.Write(WriteRequest{Actor: ActorAgent, Data: []byte("hello")})
	if !errors.Is(writeErr, io.ErrClosedPipe) || written != 2 {
		t.Fatalf("Write() = (%d, %v), want (2, io.ErrClosedPipe)", written, writeErr)
	}
	activity, err := s.ReadRecentActivity(1)
	if err != nil {
		t.Fatalf("ReadRecentActivity() error = %v", err)
	}
	if len(activity.Events) != 1 || string(activity.Events[0].Data) != "he" {
		t.Errorf("partial activity = %+v, want payload he", activity)
	}
}

func TestCoreActivityCursorsAreIndependentAndReportOverflow(t *testing.T) {
	terminal := newFakeTransport()
	s, err := New("board-1", terminal, WithActivityBufferCapacity(2))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer s.Close()
	for _, payload := range []string{"one", "two", "three"} {
		if _, err := s.Write(WriteRequest{Actor: ActorUser, Data: []byte(payload)}); err != nil {
			t.Fatalf("Write(%q) error = %v", payload, err)
		}
	}

	oldCursor, err := s.ReadActivity(context.Background(), 0, 2)
	if err != nil {
		t.Fatalf("ReadActivity(old cursor) error = %v", err)
	}
	if !oldCursor.Dropped || oldCursor.Next != 3 || len(oldCursor.Events) != 2 || string(oldCursor.Events[0].Data) != "two" {
		t.Errorf("old cursor activity = %+v, want dropped events two/three through 3", oldCursor)
	}
	independent, err := s.ReadActivity(context.Background(), 2, 1)
	if err != nil {
		t.Fatalf("ReadActivity(independent cursor) error = %v", err)
	}
	if independent.Dropped || independent.Next != 3 || len(independent.Events) != 1 || string(independent.Events[0].Data) != "three" {
		t.Errorf("independent activity = %+v, want event three through 3", independent)
	}
}

func TestCoreSlowActivityConsumerDoesNotBlockWriteAndCloseReleasesWaiter(t *testing.T) {
	terminal := newFakeTransport()
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	readDone := make(chan error, 1)
	go func() {
		chunk, readErr := s.ReadActivity(context.Background(), 0, 1)
		if readErr == nil && (len(chunk.Events) != 1 || string(chunk.Events[0].Data) != "status") {
			readErr = errors.New("unexpected activity event")
		}
		readDone <- readErr
	}()
	time.Sleep(10 * time.Millisecond)
	writeDone := make(chan error, 1)
	go func() {
		_, writeErr := s.Write(WriteRequest{Actor: ActorAgent, Data: []byte("status")})
		writeDone <- writeErr
	}()
	select {
	case writeErr := <-writeDone:
		if writeErr != nil {
			t.Errorf("Write() error = %v", writeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("slow Activity consumer blocked Session.Write")
	}
	select {
	case readErr := <-readDone:
		if readErr != nil {
			t.Error(readErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Activity waiter did not receive write")
	}

	closed := make(chan error, 1)
	go func() {
		_, readErr := s.ReadActivity(context.Background(), 1, 1)
		closed <- readErr
	}()
	time.Sleep(10 * time.Millisecond)
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case readErr := <-closed:
		if !errors.Is(readErr, io.EOF) {
			t.Errorf("blocked ReadActivity() error = %v, want io.EOF", readErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not release blocked Activity waiter")
	}
}

func TestCorePublishesStructuredEventsWithoutTerminalOutput(t *testing.T) {
	terminal := newFakeTransport()
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.PublishEvent(Event{Type: EventSessionAttached, Actor: "user", Metadata: map[string]any{"client": "cli"}})
	chunk, err := s.ReadEvents(context.Background(), 0, 1)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	if len(chunk.Events) != 1 || chunk.Events[0].ID != 0 || chunk.Events[0].SessionID != "board-1" || chunk.Events[0].Type != EventSessionAttached || chunk.Events[0].Metadata["client"] != "cli" {
		t.Errorf("event = %+v, want attached event for board-1", chunk.Events)
	}
	output, err := s.ReadRecent(64)
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Data) != 0 {
		t.Errorf("terminal output = %q, want no event data", output.Data)
	}
}

func TestCoreEventCursorsAreIndependentForMultipleObservers(t *testing.T) {
	terminal := newFakeTransport()
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.PublishEvent(Event{Type: EventSessionAttached, Actor: "user"})
	s.PublishEvent(Event{Type: EventLeaseAcquired, Actor: "system"})

	first, err := s.ReadEvents(context.Background(), 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ReadEvents(context.Background(), 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 2 || len(second.Events) != 2 || first.Events[0].Type != second.Events[0].Type || first.Next != second.Next {
		t.Errorf("independent event reads = %+v / %+v, want identical two-event snapshots", first, second)
	}
}

func TestCoreSlowEventObserverDoesNotBlockPublish(t *testing.T) {
	terminal := newFakeTransport()
	s, err := New("board-1", terminal, WithEventBufferCapacity(2))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	published := make(chan struct{})
	go func() {
		for range 3 {
			s.PublishEvent(Event{Type: EventFileTransferProgress, Actor: "user"})
		}
		close(published)
	}()
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("slow event observer blocked Session event publication")
	}
	chunk, err := s.ReadEvents(context.Background(), 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !chunk.Dropped || len(chunk.Events) != 2 || chunk.Next != 3 {
		t.Errorf("slow observer chunk = %+v, want dropped tail through cursor 3", chunk)
	}
}

func TestCoreCloseIsIdempotentAndStopsReader(t *testing.T) {
	terminal := newFakeTransport()
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	for range 2 {
		if err := s.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
	if got := terminal.closeCount(); got != 1 {
		t.Errorf("Close() calls = %d, want 1", got)
	}
	if got := s.State(); got != StateClosed {
		t.Errorf("State() = %s, want %s", got, StateClosed)
	}
	if _, err := s.ReadRecent(1); !errors.Is(err, ErrNotOpen) {
		t.Errorf("ReadRecent() error = %v, want ErrNotOpen", err)
	}
}

func TestCoreRejectsIOOutsideOpenState(t *testing.T) {
	s, err := New("board-1", newFakeTransport())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := s.Write(WriteRequest{Actor: ActorUser, Data: []byte("status")}); !errors.Is(err, ErrNotOpen) {
		t.Errorf("Write() error = %v, want ErrNotOpen", err)
	}
	if err := s.Resize(80, 24); !errors.Is(err, ErrNotOpen) {
		t.Errorf("Resize() error = %v, want ErrNotOpen", err)
	}
	if _, err := s.ReadOutput(context.Background(), 0, 1); !errors.Is(err, ErrNotOpen) {
		t.Errorf("ReadOutput() error = %v, want ErrNotOpen", err)
	}
}

func TestCoreConnectHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, err := New("board-1", newFakeTransport())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Connect() error = %v, want context.Canceled", err)
	}
	if got := s.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s", got, StateFailed)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestCoreConnectRejectsNilChannel(t *testing.T) {
	s, err := New("board-1", nilChannelTransport{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); !errors.Is(err, ErrNilChannel) {
		t.Errorf("Connect() error = %v, want ErrNilChannel", err)
	}
	if got := s.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s", got, StateFailed)
	}
}

func TestCoreTreatsResizeAsOptionalChannelCapability(t *testing.T) {
	underlying := newFakeTransport()
	stream, err := channel.NewStream(underlying)
	if err != nil {
		t.Fatalf("channel.NewStream() error = %v", err)
	}
	s, err := New("board-1", fixedChannelTransport{stream: stream})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := s.Resize(80, 24); !errors.Is(err, channel.ErrResizeUnsupported) {
		t.Errorf("Resize() error = %v, want channel.ErrResizeUnsupported", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestCoreReadOutputHonorsContextCancellation(t *testing.T) {
	terminal := newFakeTransport()
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ReadOutput(ctx, 0, 1); !errors.Is(err, context.Canceled) {
		t.Errorf("ReadOutput() error = %v, want context.Canceled", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestCoreWriteKeepsShortWritePayloadsAtomic(t *testing.T) {
	terminal := newShortWriteTransport()
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer s.Close()

	start := make(chan struct{})
	var writers sync.WaitGroup
	for _, payload := range [][]byte{[]byte("abcdef"), []byte("123456")} {
		payload := payload
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			if n, writeErr := s.Write(WriteRequest{Actor: ActorAgent, Data: payload}); writeErr != nil || n != len(payload) {
				t.Errorf("Write(%q) = (%d, %v), want (%d, nil)", payload, n, writeErr, len(payload))
			}
		}()
	}
	close(start)
	writers.Wait()

	if got := string(terminal.writtenData()); got != "abcdef123456" && got != "123456abcdef" {
		t.Errorf("short-write sequence = %q, want one complete payload followed by the other", got)
	}
}

func TestCoreCloseDoesNotDeadlockWithWrite(t *testing.T) {
	terminal := newBlockedWriteTransport()
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	writeDone := make(chan error, 1)
	go func() {
		_, writeErr := s.Write(WriteRequest{Actor: ActorUser, Data: []byte("status")})
		writeDone <- writeErr
	}()
	select {
	case <-terminal.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("Write() did not reach the transport")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- s.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Errorf("Close() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() deadlocked with an in-flight Write()")
	}
	select {
	case err := <-writeDone:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("Write() error = %v, want io.ErrClosedPipe", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Write() did not return after Close()")
	}
}

func TestCoreReadOutputWakesMultipleWaitersAndClose(t *testing.T) {
	terminal := newFakeTransport()
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	results := make(chan OutputChunk, 2)
	readErrors := make(chan error, 2)
	for range 2 {
		go func() {
			chunk, readErr := s.ReadOutput(context.Background(), 0, 16)
			if readErr != nil {
				readErrors <- readErr
				return
			}
			results <- chunk
		}()
	}
	time.Sleep(10 * time.Millisecond)
	terminal.emit([]byte("ready"))
	for range 2 {
		select {
		case readErr := <-readErrors:
			t.Fatalf("ReadOutput() error = %v", readErr)
		case chunk := <-results:
			if string(chunk.Data) != "ready" || chunk.Next != 5 {
				t.Errorf("ReadOutput() = %+v, want ready through cursor 5", chunk)
			}
		case <-time.After(time.Second):
			t.Fatal("multiple waiters were not woken by output")
		}
	}

	closed := make(chan error, 1)
	go func() {
		_, readErr := s.ReadOutput(context.Background(), 5, 16)
		closed <- readErr
	}()
	time.Sleep(10 * time.Millisecond)
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case readErr := <-closed:
		if !errors.Is(readErr, io.EOF) {
			t.Errorf("blocked ReadOutput() error = %v, want io.EOF", readErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not wake blocked ReadOutput()")
	}
}

func TestCoreReaderOverwritesOutputForSlowConsumer(t *testing.T) {
	terminal := newFakeTransport()
	s, err := New("board-1", terminal, WithReceiveBufferCapacity(4))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer s.Close()

	terminal.emit([]byte("abcd"))
	terminal.emit([]byte("efgh"))
	deadline := time.Now().Add(time.Second)
	for {
		recent, readErr := s.receive.readRecent(4)
		if readErr == nil && string(recent.Data) == "efgh" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reader did not retain the newest output")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	chunk, err := s.ReadOutput(ctx, 0, 4)
	if err != nil {
		t.Fatalf("ReadOutput() error = %v", err)
	}
	if got := string(chunk.Data); got != "efgh" || !chunk.Dropped {
		t.Errorf("ReadOutput() = %+v, want Data=efgh Dropped=true", chunk)
	}
}

func TestCoreCloseStopsZeroProgressReader(t *testing.T) {
	terminal := &zeroProgressTransport{}
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for terminal.reads.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("reader did not call Transport.Read")
		}
		time.Sleep(time.Millisecond)
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- s.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Errorf("Close() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not stop zero-progress reader")
	}
}

func TestCoreHandlesDeviceReadFailure(t *testing.T) {
	deviceErr := errors.New("device disconnected")
	terminal := &failingTransport{readErr: deviceErr}
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	select {
	case <-s.readerDone:
	case <-time.After(time.Second):
		t.Fatal("reader did not exit after device failure")
	}
	if got := s.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s", got, StateFailed)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
	if got := terminal.closeCount(); got != 1 {
		t.Errorf("Transport.Close() calls = %d, want 1", got)
	}
}

func TestCoreKeepsFixedBufferDuringHighVolumeOutput(t *testing.T) {
	const overflow = 4 * 1024 * 1024
	total := DefaultReceiveBufferSize + overflow
	terminal := &burstTransport{remaining: total}
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	select {
	case <-s.readerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not finish high-volume output")
	}

	s.receive.mu.Lock()
	capacity := len(s.receive.data)
	size := s.receive.size
	base := s.receive.base
	s.receive.mu.Unlock()
	if capacity != DefaultReceiveBufferSize || size != DefaultReceiveBufferSize {
		t.Errorf("buffer capacity/size = %d/%d, want %d/%d", capacity, size, DefaultReceiveBufferSize, DefaultReceiveBufferSize)
	}
	if wantBase := OutputCursor(overflow); base != wantBase {
		t.Errorf("buffer base = %d, want %d after overwrite", base, wantBase)
	}
	if terminal.reads.Load() == 0 {
		t.Error("reader did not consume high-volume transport output")
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestNewRejectsInvalidArguments(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		transport *fakeTransport
		option    Option
		wantErr   error
	}{
		{name: "empty ID", transport: newFakeTransport(), wantErr: ErrInvalidID},
		{name: "nil transport", id: "board-1", wantErr: ErrNilTransport},
		{name: "invalid capacity", id: "board-1", transport: newFakeTransport(), option: WithReceiveBufferCapacity(0), wantErr: ErrInvalidBufferCapacity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.id, tt.transport, tt.option)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("New() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

type nilChannelTransport struct{}

func (nilChannelTransport) Connect(context.Context) (channel.Channel, error) { return nil, nil }

type fixedChannelTransport struct {
	stream channel.Channel
}

func (t fixedChannelTransport) Connect(context.Context) (channel.Channel, error) {
	return t.stream, nil
}

// fakeTransport provides controllable blocking reads and recorded writes so
// Session lifecycle tests exercise concurrency without a physical endpoint.
type fakeTransport struct {
	mu sync.Mutex

	output chan []byte
	closed chan struct{}
	once   sync.Once

	written    []byte
	cols, rows uint16
	closes     int
}

// shortWriteTransport accepts one byte per Write to make Core exercise its
// retry loop. Its small delay gives competing callers an opportunity to expose
// interleaving if that loop were not protected by Core.writeMu.
type shortWriteTransport struct {
	*fakeTransport
}

// partialWriteTransport reports a successful prefix and an error in one call,
// exercising the Activity rule that failed writes retain only confirmed bytes.
type partialWriteTransport struct{ *fakeTransport }

func (t *partialWriteTransport) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.written = append(t.written, p[:2]...)
	t.mu.Unlock()
	return 2, io.ErrClosedPipe
}

func newShortWriteTransport() *shortWriteTransport {
	return &shortWriteTransport{fakeTransport: newFakeTransport()}
}

func (t *shortWriteTransport) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	t.mu.Lock()
	t.written = append(t.written, p[0])
	t.mu.Unlock()
	time.Sleep(time.Millisecond)
	return 1, nil
}

// blockedWriteTransport waits in Write until Close releases it, exercising the
// lock ordering that lets Close make progress while Core.writeMu is held.
type blockedWriteTransport struct {
	writeStarted chan struct{}
	closed       chan struct{}
	closeOnce    sync.Once
}

func newBlockedWriteTransport() *blockedWriteTransport {
	return &blockedWriteTransport{writeStarted: make(chan struct{}), closed: make(chan struct{})}
}

func (t *blockedWriteTransport) Connect(context.Context) (channel.Channel, error) { return t, nil }
func (t *blockedWriteTransport) Read([]byte) (int, error) {
	<-t.closed
	return 0, io.EOF
}
func (t *blockedWriteTransport) Write([]byte) (int, error) {
	select {
	case <-t.writeStarted:
	default:
		close(t.writeStarted)
	}
	<-t.closed
	return 0, io.ErrClosedPipe
}
func (t *blockedWriteTransport) Resize(uint16, uint16) error { return nil }
func (t *blockedWriteTransport) Close() error {
	t.closeOnce.Do(func() { close(t.closed) })
	return nil
}
func (*blockedWriteTransport) State() channel.State { return channel.StateOpen }

// newFakeTransport creates independent channels for each test so Close can
// deterministically release a blocked Read.
func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		output: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (f *fakeTransport) Connect(context.Context) (channel.Channel, error) { return f, nil }

func (f *fakeTransport) Read(p []byte) (int, error) {
	select {
	case data := <-f.output:
		return copy(p, data), nil
	case <-f.closed:
		return 0, io.EOF
	}
}

func (f *fakeTransport) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = append(f.written, p...)
	return len(p), nil
}

func (f *fakeTransport) Resize(cols, rows uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cols, f.rows = cols, rows
	return nil
}

func (f *fakeTransport) Close() error {
	f.mu.Lock()
	f.closes++
	f.mu.Unlock()
	f.once.Do(func() { close(f.closed) })
	return nil
}

func (*fakeTransport) State() channel.State { return channel.StateOpen }

func (f *fakeTransport) emit(data []byte) {
	f.output <- append([]byte(nil), data...)
}

func (f *fakeTransport) writtenData() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.written...)
}

func (f *fakeTransport) dimensions() (uint16, uint16) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cols, f.rows
}

func (f *fakeTransport) closeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closes
}

// zeroProgressTransport violates io.Reader progress rules to verify Core's
// idle delay prevents a faulty driver from creating a CPU spin.
type zeroProgressTransport struct {
	reads atomic.Int64
}

func (t *zeroProgressTransport) Connect(context.Context) (channel.Channel, error) { return t, nil }
func (t *zeroProgressTransport) Read([]byte) (int, error) {
	t.reads.Add(1)
	return 0, nil
}
func (t *zeroProgressTransport) Write(p []byte) (int, error) { return len(p), nil }
func (t *zeroProgressTransport) Resize(uint16, uint16) error { return nil }
func (t *zeroProgressTransport) Close() error                { return nil }
func (*zeroProgressTransport) State() channel.State          { return channel.StateOpen }

// failingTransport returns a prescribed reader failure to test Core's failed
// state and error propagation independently of transport implementation.
type failingTransport struct {
	readErr error
	closes  atomic.Int64
}

func (t *failingTransport) Connect(context.Context) (channel.Channel, error) { return t, nil }
func (t *failingTransport) Read([]byte) (int, error)                         { return 0, t.readErr }
func (t *failingTransport) Write(p []byte) (int, error)                      { return len(p), nil }
func (t *failingTransport) Resize(uint16, uint16) error                      { return nil }
func (t *failingTransport) Close() error {
	t.closes.Add(1)
	return nil
}
func (t *failingTransport) closeCount() int    { return int(t.closes.Load()) }
func (*failingTransport) State() channel.State { return channel.StateOpen }

// burstTransport emits a finite stream without blocking so tests can exercise
// high-volume reader behavior and buffer retention deterministically.
type burstTransport struct {
	remaining int
	reads     atomic.Int64
}

func (t *burstTransport) Connect(context.Context) (channel.Channel, error) { return t, nil }
func (t *burstTransport) Read(p []byte) (int, error) {
	if t.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), t.remaining)
	for i := range p[:n] {
		p[i] = byte(t.remaining)
	}
	t.remaining -= n
	t.reads.Add(1)
	return n, nil
}
func (t *burstTransport) Write(p []byte) (int, error) { return len(p), nil }
func (t *burstTransport) Resize(uint16, uint16) error { return nil }
func (t *burstTransport) Close() error                { return nil }
func (*burstTransport) State() channel.State          { return channel.StateOpen }

func (t *shortWriteTransport) Connect(context.Context) (channel.Channel, error) {
	return t, nil
}

func (t *partialWriteTransport) Connect(context.Context) (channel.Channel, error) {
	return t, nil
}
