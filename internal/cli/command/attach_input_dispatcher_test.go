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

	"github.com/akira-init1/ChannelTerm/internal/core/session"
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
		if err := wrapped.ReportFileTransferEvent(context.Background(), session.EventFileTransferProgress, map[string]any{"sent": int64(909312), "total": int64(1572864), "percent": 57.8}); err != nil {
			return err
		}
		<-wrapped.cancellation.ch
		if !cancelRequested() {
			return fmt.Errorf("dispatcher cancellation latch was not propagated")
		}
		close(transferCancelled)
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
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(output.String(), "Cancel file transfer? [y/N]:") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if strings.Contains(output.String(), "File transfer cancelled") {
		t.Fatalf("output = %q, Ctrl+C must wait for confirmation", output.String())
	}
	if _, err := inputWriter.Write([]byte{'y'}); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, transferCancelled, "transfer cancellation request")
	waitForSignal(t, workerDone, "transfer worker completion")

	// The dispatcher changes back to attach only after the worker has completed
	// its cleanup. A brief recovery window then consumes any auto-repeated
	// Ctrl+C from the cancellation gesture before normal remote input resumes.
	deadline = time.Now().Add(time.Second)
	for !strings.Contains(output.String(), "Reason     : cancelled by user") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(output.String(), "Reason     : cancelled by user") {
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
	if got := output.String(); strings.Count(got, "Cancel file transfer? [y/N]:") != 1 || strings.Contains(got, "Cancelling after the active transfer block...") || !strings.Contains(got, "Transferred: 909312/1572864 bytes (57.8%)\r\n  Reason     : cancelled by user") {
		t.Errorf("output = %q, want one confirmation prompt and the confirmed-progress cancellation summary", got)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, dispatchDone, "dispatcher exit")
}

func TestAttachInputDispatcherDeclinesCancellationAndResumesAtBoundary(t *testing.T) {
	for _, tt := range []struct {
		name   string
		answer []byte
	}{
		{name: "lowercase n", answer: []byte{'n'}},
		{name: "uppercase N", answer: []byte{'N'}},
		{name: "enter default", answer: []byte{'\r'}},
		{name: "other character", answer: []byte{'x'}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			inputReader, inputWriter := io.Pipe()
			defer inputReader.Close()
			defer inputWriter.Close()
			pump := newAttachInputPump(inputReader)
			var output lockedBuffer
			started := make(chan struct{})
			checkBoundary := make(chan struct{})
			boundaryResult := make(chan bool, 1)
			finishWorker := make(chan struct{})
			attached := &cancellableShortcutSession{}
			dispatcher := newAttachInputDispatcherWithPump(context.Background(), &pump, attached, func(data []byte) error {
				_, err := output.Write(data)
				return err
			}, func() error { return nil }, func() {}, func(workerCtx context.Context, transfer attachSession, _ io.Writer, cancelRequested func() bool, _ string, _, _ string) error {
				lease := transfer.(fileLeaseSession)
				if err := lease.AcquireFileTransferLease(workerCtx); err != nil {
					return err
				}
				defer func() { _ = lease.ReleaseFileTransferLease(context.Background()) }()
				close(started)
				<-checkBoundary
				boundaryResult <- cancelRequested()
				<-finishWorker
				return nil
			})
			done := make(chan struct{})
			go func() {
				dispatcher.run()
				close(done)
			}()

			if _, err := inputWriter.Write([]byte("\x1dfssources.bin\n\n")); err != nil {
				t.Fatal(err)
			}
			waitForSignal(t, started, "file-transfer worker start")
			if _, err := inputWriter.Write([]byte{0x03}); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for !strings.Contains(output.String(), "Cancel file transfer? [y/N]:") && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			close(checkBoundary)
			select {
			case result := <-boundaryResult:
				t.Fatalf("boundary returned %t before confirmation answer", result)
			case <-time.After(20 * time.Millisecond):
			}
			if got := attached.lifecycle(); len(got) != 1 || got[0] != "acquire" {
				t.Fatalf("lease lifecycle during confirmation = %v, want held lease", got)
			}
			if _, err := inputWriter.Write(tt.answer); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-boundaryResult:
				if result {
					t.Fatal("declined confirmation requested cancellation")
				}
			case <-time.After(time.Second):
				t.Fatal("transfer did not resume after declined confirmation")
			}
			if got := attached.lifecycle(); len(got) != 1 || got[0] != "acquire" {
				t.Fatalf("lease lifecycle after resume = %v, want lease held until transfer completion", got)
			}
			deadline = time.Now().Add(time.Second)
			for !strings.Contains(output.String(), "[ChannelTerm] File transfer resumed") && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if got := output.String(); strings.Count(got, "Cancel file transfer? [y/N]:") != 1 || strings.Count(got, "[ChannelTerm] File transfer resumed") != 1 {
				t.Errorf("output = %q, want one prompt and one resumed status", got)
			}
			close(finishWorker)
			deadline = time.Now().Add(time.Second)
			for !strings.Contains(output.String(), "File transfer completed") && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if got := attached.lifecycle(); len(got) != 2 || got[0] != "acquire" || got[1] != "release" {
				t.Errorf("final lease lifecycle = %v, want acquire/release", got)
			}
			if err := inputWriter.Close(); err != nil {
				t.Fatal(err)
			}
			waitForSignal(t, done, "dispatcher exit")
		})
	}
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

func TestFileTransferCancellationPresentationText(t *testing.T) {
	if got, want := string(fileTransferCancelConfirmationText), "\r\n^C\r\n[ChannelTerm] Cancel file transfer? [y/N]: "; got != want {
		t.Errorf("confirmation prompt = %q, want %q", got, want)
	}
	if got, want := string(fileTransferResumedText), "\r\n[ChannelTerm] File transfer resumed\r\n"; got != want {
		t.Errorf("resumed text = %q, want %q", got, want)
	}
	timestamp := time.Date(2026, time.September, 15, 9, 50, 27, 0, time.Local)
	progress := fileTransferCancellationSnapshot{transferred: 909312, total: 1572864, percent: 57.8}
	want := "[09:50:27] [ChannelTerm] File transfer cancelled\r\n" +
		"  Transferred: 909312/1572864 bytes (57.8%)\r\n" +
		"  Reason     : cancelled by user\r\n"
	if got := string(fileTransferCancelledSummaryText(timestamp, progress)); got != want {
		t.Errorf("cancelled summary = %q, want %q", got, want)
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

func TestAttachInputDispatcherCancelsExternallyOwnedFileTransfer(t *testing.T) {
	for _, tt := range []struct {
		name       string
		answer     byte
		wantCancel bool
	}{
		{name: "confirm", answer: 'Y', wantCancel: true},
		{name: "default resume", answer: '\r', wantCancel: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			inputReader, inputWriter := io.Pipe()
			defer inputReader.Close()
			pump := newAttachInputPump(inputReader)
			attached := &externalTransferAttachSession{
				fakeAttachSession: &fakeAttachSession{},
				resolved:          make(chan bool, 1),
			}
			var output lockedBuffer
			dispatcher := newAttachInputDispatcherWithPump(context.Background(), &pump, attached, func(data []byte) error {
				_, err := output.Write(data)
				return err
			}, func() error { return nil }, func() {}, nil)
			done := make(chan struct{})
			go func() {
				dispatcher.run()
				close(done)
			}()
			if _, err := inputWriter.Write([]byte{0x03}); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for !strings.Contains(output.String(), "Cancel file transfer? [y/N]:") && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if _, err := inputWriter.Write([]byte{tt.answer}); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-attached.resolved:
				if got != tt.wantCancel {
					t.Fatalf("resolved cancel = %t, want %t", got, tt.wantCancel)
				}
			case <-time.After(time.Second):
				t.Fatal("external confirmation was not resolved")
			}
			if got := attached.writtenData(); len(got) != 0 {
				t.Fatalf("board input = %x, external transfer Ctrl+C must stay local", got)
			}
			if !tt.wantCancel && !strings.Contains(output.String(), "File transfer resumed") {
				t.Fatalf("output = %q, want resumed status", output.String())
			}
			if err := inputWriter.Close(); err != nil {
				t.Fatal(err)
			}
			waitForSignal(t, done, "dispatcher exit")
		})
	}
}

func TestAttachInputDispatcherIgnoresNormalInputDuringFileTransferOnce(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	pump := newAttachInputPump(inputReader)
	attached := &fakeAttachSession{}
	var output lockedBuffer
	started := make(chan struct{})
	releaseWorker := make(chan struct{})
	dispatcher := newAttachInputDispatcherWithPump(context.Background(), &pump, attached, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, func() error { return nil }, func() {}, func(_ context.Context, _ attachSession, _ io.Writer, _ func() bool, _ string, _, _ string) error {
		close(started)
		<-releaseWorker
		return nil
	})
	done := make(chan struct{})
	go func() {
		dispatcher.run()
		close(done)
	}()

	if _, err := inputWriter.Write([]byte("\x1dfssources.bin\n\n")); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, started, "file-transfer worker start")
	if _, err := inputWriter.Write([]byte("x\r\ny")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if got := attached.writtenData(); len(got) != 0 {
		t.Errorf("board input during transfer = %q, want none", got)
	}
	if got := output.String(); strings.Count(got, "Input ignored during file transfer. Ctrl+C to cancel.") != 1 {
		t.Errorf("output = %q, want one input-ignored hint", got)
	}
	for _, want := range []string{
		"File transfer started: sources.bin -> /tmp/sources.bin",
		"Terminal input locked. Ctrl+C to cancel.",
	} {
		if got := output.String(); !strings.Contains(got, want) {
			t.Errorf("output = %q, want %q", got, want)
		}
	}

	close(releaseWorker)
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(output.String(), "File transfer completed") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := inputWriter.Write([]byte("z\n")); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for string(attached.writtenData()) != "z\n" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := string(attached.writtenData()); got != "z\n" {
		t.Errorf("board input after transfer = %q, want normal input restored", got)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, done, "dispatcher exit")
}

func TestAttachInputDispatcherResetsInputIgnoredHintForNextTransfer(t *testing.T) {
	attached := &fakeAttachSession{}
	var output lockedBuffer
	pump := newAttachInputPump(bytes.NewReader(nil))
	dispatcher := newAttachInputDispatcherWithPump(context.Background(), &pump, attached, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, func() error { return nil }, func() {}, nil)
	dispatcher.mode = attachInputModeFileTransfer
	if !dispatcher.ignoreTransferInput() || !dispatcher.ignoreTransferInput() {
		t.Fatal("ignoreTransferInput() returned false")
	}
	dispatcher.finishTransfer(nil)
	dispatcher.mode = attachInputModeFileTransfer
	if !dispatcher.ignoreTransferInput() {
		t.Fatal("ignoreTransferInput() after reset returned false")
	}
	if got := output.String(); strings.Count(got, "Input ignored during file transfer. Ctrl+C to cancel.") != 2 {
		t.Errorf("output = %q, want one hint per transfer", got)
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
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(output.String(), "Cancel file transfer? [y/N]:") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := inputWriter.Write([]byte{'y'}); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, cancelled, "unified transfer cancellation")
	deadline = time.Now().Add(time.Second)
	for len(interrupts) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(interrupts) != 0 {
		t.Fatal("process-level Ctrl+C was not routed through the dispatcher")
	}
	if got := attached.writtenData(); bytes.Contains(got, []byte{0x03}) {
		t.Errorf("board input = %x, transfer Ctrl+C must not be forwarded", got)
	}
	if got := output.String(); strings.Count(got, "Cancel file transfer? [y/N]:") != 1 {
		t.Errorf("output = %q, want one confirmation prompt for raw and process Ctrl+C", got)
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
	for !strings.Contains(output.String(), "Cancel file transfer? [y/N]:") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := inputWriter.Write([]byte{'y'}); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(time.Second)
	for !strings.Contains(output.String(), "File transfer cancelled") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := output.String(); !strings.Contains(got, "File transfer cancelled") {
		t.Fatalf("output = %q, want transfer cancellation status", got)
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

// TestAttachInputDispatcherDropsControlCAfterSlowTransferCleanup verifies the
// recovery window starts after the worker has safely completed. Windows can
// deliver the plain-C half of Ctrl+C only after a slow active transfer block
// has finished, well after the original key event.
func TestAttachInputDispatcherDropsControlCAfterSlowTransferCleanup(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	pump := newAttachInputPump(inputReader)
	attached := &fakeAttachSession{}
	var output lockedBuffer
	started := make(chan struct{})
	releaseWorker := make(chan struct{})
	dispatcher := newAttachInputDispatcherWithPump(context.Background(), &pump, attached, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, func() error { return nil }, func() {}, func(_ context.Context, transfer attachSession, _ io.Writer, _ func() bool, _ string, _, _ string) error {
		close(started)
		<-transfer.(nonClosingAttachSession).cancellation.ch
		<-releaseWorker
		return errors.New("slow transfer worker stopped")
	})
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
	for !strings.Contains(output.String(), "Cancel file transfer? [y/N]:") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := inputWriter.Write([]byte{'Y'}); err != nil {
		t.Fatal(err)
	}
	// Model an active raw block that takes longer than the former recovery
	// window to complete.
	time.Sleep(controlCRecoveryWindow + 10*time.Millisecond)
	close(releaseWorker)

	deadline = time.Now().Add(time.Second)
	for !strings.Contains(output.String(), "File transfer cancelled") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(output.String(), "File transfer cancelled") {
		t.Fatal("transfer did not complete")
	}
	if _, err := inputWriter.Write([]byte{'c'}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if got := attached.writtenData(); len(got) != 0 {
		t.Errorf("board input = %x, want delayed Ctrl+C record discarded", got)
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

type externalTransferAttachSession struct {
	*fakeAttachSession
	resolved chan bool
}

func (*externalTransferAttachSession) BeginFileTransferCancel(context.Context) (string, bool, error) {
	return "request-1", true, nil
}

func (s *externalTransferAttachSession) ResolveFileTransferCancel(_ context.Context, requestID string, cancel bool) (string, error) {
	if requestID != "request-1" {
		return "", fmt.Errorf("unexpected request ID %q", requestID)
	}
	s.resolved <- cancel
	if cancel {
		return "cancelled", nil
	}
	return "resumed", nil
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
