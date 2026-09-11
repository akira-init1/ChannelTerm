package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAttachInputDispatcherRoutesControlCByMode(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	pump := newAttachInputPump(inputReader)
	attached := &fakeAttachSession{}
	var output lockedBuffer
	transferStarted := make(chan struct{})
	transferCancelled := make(chan struct{})
	workerDone := make(chan struct{})
	writeLocal := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	dispatcher := newAttachInputDispatcherWithPump(context.Background(), &pump, attached, writeLocal, func() error { return nil }, func() {}, func(_ context.Context, transfer attachSession, workerOutput io.Writer, cancelRequested func() bool, _ string, _, _ string) error {
		close(transferStarted)
		wrapped := transfer.(nonClosingAttachSession)
		<-wrapped.cancellation.ch
		if !cancelRequested() {
			return fmt.Errorf("dispatcher cancellation latch was not propagated")
		}
		close(transferCancelled)
		if _, err := fmt.Fprintln(workerOutput, "Transfer cancelled."); err != nil {
			return err
		}
		close(workerDone)
		return context.Canceled
	})
	dispatchDone := make(chan struct{})
	go func() {
		dispatcher.run()
		close(dispatchDone)
	}()

	if _, err := inputWriter.Write([]byte{0x1d, 'f', 's'}); err != nil {
		t.Fatal(err)
	}
	if _, err := inputWriter.Write([]byte("firmware.bin\n\n")); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, transferStarted, "file-transfer worker start")
	if _, err := inputWriter.Write([]byte{0x03}); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, transferCancelled, "transfer cancellation request")
	waitForSignal(t, workerDone, "transfer worker completion")

	// The dispatcher changes back to attach only after the worker has completed
	// its cleanup. A brief recovery window then consumes any auto-repeated
	// Ctrl+C from the cancellation gesture before normal remote input resumes.
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(output.String(), "Transfer cancelled.") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(output.String(), "Transfer cancelled.") {
		t.Fatalf("output = %q, want completed cancellation status", output.String())
	}
	time.Sleep(controlCRecoveryWindow)
	if _, err := inputWriter.Write([]byte{0x03}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for len(attached.writtenData()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := attached.writtenData(); !bytes.Equal(got, []byte{0x03}) {
		t.Errorf("board input = %x, want only Ctrl+C after transfer completion", got)
	}
	if got := output.String(); strings.Count(got, "Cancelling after the active transfer block...") != 1 || strings.Count(got, "Transfer cancelled.") != 1 {
		t.Errorf("output = %q, want one cancellation request and one final status", got)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, dispatchDone, "dispatcher exit")
}

func TestFileTransferProgressWithCancellationStopsAtAcknowledgedBoundary(t *testing.T) {
	requested := false
	updates := 0
	progress := fileTransferProgressWithCancellation(func(int64, int64) error {
		updates++
		return nil
	}, func() bool { return requested })
	if err := progress(32*1024, 64*1024); err != nil {
		t.Fatalf("first acknowledged progress = %v", err)
	}
	requested = true
	if err := progress(64*1024, 64*1024); !errors.Is(err, context.Canceled) {
		t.Fatalf("progress after cancellation = %v, want context.Canceled", err)
	}
	if updates != 1 {
		t.Errorf("progress updates = %d, want one before cancellation", updates)
	}
}

func TestAttachInputDispatcherRoutesAttachControlCToBoard(t *testing.T) {
	pump := newAttachInputPump(bytes.NewReader([]byte{0x03}))
	attached := &fakeAttachSession{}
	dispatcher := newAttachInputDispatcherWithPump(context.Background(), &pump, attached, func([]byte) error { return nil }, func() error { return nil }, func() {}, nil)
	dispatcher.run()
	if got := attached.writtenData(); !bytes.Equal(got, []byte{0x03}) {
		t.Errorf("board input = %x, want Ctrl+C byte", got)
	}
}

func TestAttachInputDispatcherDeduplicatesRawAndProcessControlCForTransfer(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	pump := newAttachInputPump(inputReader)
	interrupts := make(chan os.Signal, 1)
	attached := &fakeAttachSession{}
	var output lockedBuffer
	started := make(chan struct{})
	cancelled := make(chan struct{})
	releaseWorker := make(chan struct{})
	dispatcher := newAttachInputDispatcherWithPumpAndInterrupts(context.Background(), &pump, attached, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, func() error { return nil }, func() {}, func(_ context.Context, transfer attachSession, workerOutput io.Writer, _ func() bool, _ string, _, _ string) error {
		close(started)
		<-transfer.(nonClosingAttachSession).cancellation.ch
		close(cancelled)
		<-releaseWorker
		_, _ = fmt.Fprintln(workerOutput, "Transfer cancelled.")
		return context.Canceled
	}, interrupts)
	done := make(chan struct{})
	go func() {
		dispatcher.run()
		close(done)
	}()
	if _, err := inputWriter.Write([]byte("\x1dfsources.bin\n\n")); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, started, "file-transfer worker start")
	if _, err := inputWriter.Write([]byte{0x03}); err != nil {
		t.Fatal(err)
	}
	interrupts <- os.Interrupt
	waitForSignal(t, cancelled, "unified transfer cancellation")
	deadline := time.Now().Add(time.Second)
	for len(interrupts) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(interrupts) != 0 {
		t.Fatal("process-level Ctrl+C was not routed through the dispatcher")
	}
	if got := attached.writtenData(); bytes.Contains(got, []byte{0x03}) {
		t.Errorf("board input = %x, transfer Ctrl+C must not be forwarded", got)
	}
	deadline = time.Now().Add(time.Second)
	for strings.Count(output.String(), "Cancelling after the active transfer block...") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := output.String(); strings.Count(got, "Cancelling after the active transfer block...") != 1 {
		t.Errorf("output = %q, want one cancellation request for raw and process Ctrl+C", got)
	}
	close(releaseWorker)
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, done, "dispatcher exit")
}

func TestAttachInputDispatcherDropsDelayedDuplicateControlCAfterTransfer(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	pump := newAttachInputPump(inputReader)
	interrupts := make(chan os.Signal, 1)
	attached := &fakeAttachSession{}
	var output lockedBuffer
	started := make(chan struct{})
	dispatcher := newAttachInputDispatcherWithPumpAndInterrupts(context.Background(), &pump, attached, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, func() error { return nil }, func() {}, func(_ context.Context, transfer attachSession, _ io.Writer, _ func() bool, _ string, _, _ string) error {
		close(started)
		<-transfer.(nonClosingAttachSession).cancellation.ch
		return errors.New("transfer worker stopped")
	}, interrupts)
	done := make(chan struct{})
	go func() {
		dispatcher.run()
		close(done)
	}()
	if _, err := inputWriter.Write([]byte("\x1dfsources.bin\n\n")); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, started, "file-transfer worker start")
	if _, err := inputWriter.Write([]byte{0x03}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for !strings.Contains(output.String(), "transfer worker stopped") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := output.String(); !strings.Contains(got, "transfer worker stopped") {
		t.Fatalf("output = %q, want transfer completion", got)
	}
	interrupts <- os.Interrupt
	if _, err := inputWriter.Write([]byte{'c', 'c', 'c', 'c', 0x03, 0x03, 0x03, 0x03}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if got := attached.writtenData(); len(got) != 0 {
		t.Errorf("board input = %x, want no repeated Ctrl+C after cancellation", got)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, done, "dispatcher exit")
}

func TestAttachInputDispatcherUsesSingleReaderForMenuAndPaths(t *testing.T) {
	input := &countingInputReader{data: []byte{0x1d, 'f', 's', 'a', '.', 'b', 'i', 'n', '\r', '\n', 0x1b}}
	pump := newAttachInputPump(input)
	var output lockedBuffer
	writeLocal := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	dispatcher := newAttachInputDispatcherWithPump(context.Background(), &pump, &fakeAttachSession{}, writeLocal, func() error { return nil }, func() {}, nil)
	dispatcher.run()
	if got := input.maxConcurrentReads(); got != 1 {
		t.Errorf("concurrent stdin reads = %d, want 1", got)
	}
	for _, want := range []string{"Select: s\r\nLocal path: a.bin\r\n", "Remote path [/tmp/a.bin]:", "File transfer cancelled"} {
		if got := output.String(); !strings.Contains(got, want) {
			t.Errorf("output = %q, want %q", got, want)
		}
	}
}

func TestAttachInputDispatcherFileMenuSingleKeyChoices(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{name: "send", input: []byte("\x1dfsa.bin\n\x1b"), want: "Select: s\r\nLocal path: a.bin\r\nRemote path [/tmp/a.bin]:"},
		{name: "receive", input: []byte("\x1dfr/var/log/app.log\n\x1b"), want: "Select: r\r\nRemote path: /var/log/app.log\r\nLocal path [./app.log]:"},
		{name: "escape", input: []byte{0x1d, 'f', 0x1b}, want: "File transfer cancelled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pump := newAttachInputPump(bytes.NewReader(tt.input))
			var output lockedBuffer
			writeLocal := func(data []byte) error {
				_, err := output.Write(data)
				return err
			}
			dispatcher := newAttachInputDispatcherWithPump(context.Background(), &pump, &fakeAttachSession{}, writeLocal, func() error { return nil }, func() {}, nil)
			dispatcher.run()
			if got := output.String(); !strings.Contains(got, tt.want) {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
			if got := output.String(); strings.Contains(got, "\r\ns\r\n") || strings.Contains(got, "\r\nr\r\n") {
				t.Errorf("output = %q, choice must not be echoed on its own line", got)
			}
		})
	}
}

type lockedBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

type countingInputReader struct {
	mu        sync.Mutex
	data      []byte
	active    int
	maxActive int
}

func (r *countingInputReader) Read(data []byte) (int, error) {
	r.mu.Lock()
	r.active++
	r.maxActive = max(r.maxActive, r.active)
	if len(r.data) == 0 {
		r.active--
		r.mu.Unlock()
		return 0, io.EOF
	}
	count := copy(data, r.data)
	r.data = r.data[count:]
	r.active--
	r.mu.Unlock()
	return count, nil
}

func (r *countingInputReader) maxConcurrentReads() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maxActive
}
