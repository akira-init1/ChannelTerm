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

func TestAttachInputDispatcherConfirmedCancellationInterruptsProtocolWait(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	pump := newAttachInputPump(inputReader)
	attached := &fakeAttachSession{}
	var output lockedBuffer
	started := make(chan struct{})
	waitCancelled := make(chan struct{})
	dispatcher := newAttachInputDispatcherWithPump(context.Background(), &pump, attached, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, func() error { return nil }, func() {}, func(workerCtx context.Context, _ attachSession, _ io.Writer, _ func() bool, _ string, _, _ string) error {
		close(started)
		<-workerCtx.Done()
		close(waitCancelled)
		return workerCtx.Err()
	})
	done := make(chan struct{})
	go func() {
		dispatcher.run()
		close(done)
	}()

	if _, err := inputWriter.Write([]byte("\x1dfssources.bin\n\n")); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, started, "blocked protocol wait")
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
	waitForSignal(t, waitCancelled, "protocol wait cancellation")
	deadline = time.Now().Add(time.Second)
	for !strings.Contains(output.String(), "File transfer cancelled") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := output.String(); !strings.Contains(got, "Reason     : cancelled by user") {
		t.Fatalf("output = %q, want completed cancellation summary", got)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, done, "dispatcher exit")
}

func TestAttachInputDispatcherContextCancellationTerminatesProgressLine(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	pump := newAttachInputPump(inputReader)
	var output lockedBuffer
	lineState := newPresentationLineState()
	writeLocal := func(data []byte) error {
		_, err := output.Write(data)
		lineState.observe(data)
		return err
	}
	writeStatus := func(data []byte) error {
		if prefix := lineState.lineStartPrefix(); len(prefix) > 0 {
			if err := writeLocal(prefix); err != nil {
				return err
			}
		}
		return writeLocal(data)
	}
	started := make(chan struct{})
	dispatcher := newAttachInputDispatcherWithPump(ctx, &pump, &fakeAttachSession{}, writeLocal, func() error { return nil }, func() {}, func(_ context.Context, transfer attachSession, workerOutput io.Writer, _ func() bool, _ string, _, _ string) error {
		progress := newFileTransferProgress(context.Background(), workerOutput, nil, map[string]any{"direction": "send"})
		if err := progress.Start(64 * 1024); err != nil {
			return err
		}
		close(started)
		<-transfer.(nonClosingAttachSession).cancellation.ch
		return finishFileTransferPresentation(workerOutput, progress, context.Canceled)
	})
	dispatcher.writeStatus = writeStatus
	dispatcher.startTransfer("send", "firmware.bin", "/tmp/firmware.bin")
	done := make(chan struct{})
	go func() {
		dispatcher.run()
		close(done)
	}()
	waitForSignal(t, started, "progress start")
	cancel()
	waitForSignal(t, done, "dispatcher context cancellation")

	got := output.String()
	if !strings.Contains(got, "\x1b[K\r\n[") || !strings.Contains(got, "File transfer cancelled") {
		t.Errorf("context cancellation output = %q, want terminated progress followed by cancellation status", got)
	}
	if strings.Contains(got, "\x1b[K\r\n\r\n[") {
		t.Errorf("context cancellation output = %q, contains a blank line after progress", got)
	}
}

func TestAttachInputDispatcherPrintsCompletionBeforeVerificationSummary(t *testing.T) {
	var output lockedBuffer
	dispatcher := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, &fakeAttachSession{}, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, func() error { return nil }, func() {}, func(_ context.Context, _ attachSession, workerOutput io.Writer, _ func() bool, _ string, _, _ string) error {
		const total = 28 * 1024
		progress := newFileTransferProgress(context.Background(), workerOutput, nil, map[string]any{"direction": "send"})
		if err := progress.Report(total, total); err != nil {
			return err
		}
		if err := progress.Complete(total); err != nil {
			return err
		}
		if err := progress.finish(); err != nil {
			return err
		}
		return writeFileTransferVerificationSummary(workerOutput, "digest", "/tmp/system_1.dtb")
	})
	dispatcher.startTransfer("send", "system.dtb", "/tmp/system.dtb")
	err := <-dispatcher.transferDone
	if got := output.String(); strings.Contains(got, "Local SHA-256") {
		t.Fatalf("worker output = %q, verification summary must wait for completion status", got)
	}
	dispatcher.finishTransfer(err)

	got := output.String()
	progressIndex := strings.Index(got, "[####################] 100.0%")
	completedIndex := strings.Index(got, "[ChannelTerm] File transfer completed")
	summaryIndex := strings.Index(got, "  Local SHA-256 : digest")
	if progressIndex < 0 || completedIndex <= progressIndex || summaryIndex <= completedIndex {
		t.Errorf("manual transfer output order is incorrect: %q", got)
	}
	if !strings.Contains(got, "File transfer completed\r\n  Local SHA-256 : digest\r\n") {
		t.Errorf("manual transfer output = %q, want summary immediately after completion without a blank line", got)
	}
	if !strings.Contains(got, "  Remote SHA-256: digest\r\n  Verify        : MATCH\r\n  Saved         : /tmp/system_1.dtb\r\n") {
		t.Errorf("manual transfer summary = %q, want MCP-compatible verification fields", got)
	}
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
	if got, want := string(fileTransferCancelConfirmationText), "\r\n[ChannelTerm] Cancel file transfer? [y/N]: "; got != want {
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

func TestFileTransferCancellationAnswerEchoesOnPromptLine(t *testing.T) {
	for _, tt := range []struct {
		name          string
		answer        byte
		wantAnswer    string
		wantCancelled bool
	}{
		{name: "confirm", answer: 'y', wantAnswer: "y", wantCancelled: true},
		{name: "decline", answer: 'n', wantAnswer: "n"},
		{name: "other", answer: 'x', wantAnswer: "x"},
		{name: "default enter", answer: '\r'},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			cancellation := newFileTransferCancellation()
			if !cancellation.BeginConfirmation() {
				t.Fatal("BeginConfirmation() returned false")
			}
			dispatcher := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, &fakeAttachSession{}, func(data []byte) error {
				_, err := output.Write(data)
				return err
			}, func() error { return nil }, func() {}, nil)
			dispatcher.mode = attachInputModeFileTransferCancelConfirm
			dispatcher.transferCancellation = cancellation
			output.Write(fileTransferCancelConfirmationText)

			if ok := dispatcher.dispatch([]byte{tt.answer}); !ok {
				t.Fatal("dispatch() returned false")
			}
			promptAndAnswer := string(fileTransferCancelConfirmationText) + tt.wantAnswer + "\r\n"
			if got := output.String(); !strings.HasPrefix(got, promptAndAnswer) {
				t.Errorf("output = %q, want prompt answer prefix %q", got, promptAndAnswer)
			}
			if cancellation.Cancelled() != tt.wantCancelled {
				t.Errorf("cancelled = %t, want %t", cancellation.Cancelled(), tt.wantCancelled)
			}
		})
	}
}

func TestFileTransferStatusPrefixUsesLocalTimezone(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("UTC+8", 8*60*60)
	defer func() { time.Local = previousLocal }()

	timestamp := time.Date(2026, time.September, 15, 8, 9, 2, 0, time.UTC)
	if got, want := fileTransferStatusPrefix(timestamp), "[16:09:02] [ChannelTerm] "; got != want {
		t.Errorf("status prefix = %q, want local time %q", got, want)
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

func TestExternalCancellationClosesProgressGateBeforeWritingPrompt(t *testing.T) {
	presentation := newFileTransferPresentation()
	if err := presentation.handle(session.Event{Type: session.EventFileTransferProgress, Metadata: map[string]any{
		"sent": int64(56 * 1024), "total": int64(1024 * 1024), "percent": 5.5, "speed": float64(8 * 1024),
	}}, false, func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	attached := &presentationAwareExternalTransferSession{
		externalTransferAttachSession: externalTransferAttachSession{fakeAttachSession: &fakeAttachSession{}, resolved: make(chan bool, 1)},
		presentation:                  presentation,
	}
	progressRedrawn := false
	dispatcher := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, attached, func(data []byte) error {
		if bytes.Equal(data, fileTransferCancelConfirmationText) {
			return presentation.handle(session.Event{Type: session.EventFileTransferProgress, Metadata: map[string]any{
				"sent": int64(64 * 1024), "total": int64(1024 * 1024), "percent": 6.25, "speed": float64(8 * 1024),
			}}, false, func([]byte) error {
				progressRedrawn = true
				return nil
			})
		}
		return nil
	}, func() error { return nil }, func() {}, nil)
	if active := dispatcher.beginExternalTransferCancellationConfirmation(); !active {
		t.Fatal("external cancellation was not detected")
	}
	if progressRedrawn {
		t.Fatal("progress redrew after the cancellation prompt became visible")
	}
	dispatcher.abandonExternalTransferCancellation()
}

func TestExternalCancellationCannotBeOvertakenByPreparedProgress(t *testing.T) {
	presentation := newFileTransferPresentation()
	attached := &presentationAwareExternalTransferSession{
		externalTransferAttachSession: externalTransferAttachSession{fakeAttachSession: &fakeAttachSession{}, resolved: make(chan bool, 1)},
		presentation:                  presentation,
	}
	var output lockedBuffer
	progressWriteStarted := make(chan struct{})
	releaseProgressWrite := make(chan struct{})
	progressDone := make(chan error, 1)
	go func() {
		progressDone <- presentation.handle(session.Event{Type: session.EventFileTransferProgress, Metadata: map[string]any{
			"sent": int64(56 * 1024), "total": int64(128 * 1024), "percent": 43.75, "speed": float64(8 * 1024),
		}}, false, func(data []byte) error {
			close(progressWriteStarted)
			<-releaseProgressWrite
			_, err := output.Write(data)
			return err
		})
	}()
	waitForSignal(t, progressWriteStarted, "prepared progress write")

	promptWritten := make(chan struct{})
	confirmationDone := make(chan bool, 1)
	dispatcher := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, attached, func(data []byte) error {
		_, err := output.Write(data)
		if err == nil && bytes.Equal(data, fileTransferCancelConfirmationText) {
			close(promptWritten)
		}
		return err
	}, func() error { return nil }, func() {}, nil)
	go func() {
		confirmationDone <- dispatcher.beginExternalTransferCancellationConfirmation()
	}()

	select {
	case <-promptWritten:
		t.Fatal("cancellation prompt overtook an already prepared progress frame")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseProgressWrite)
	if err := <-progressDone; err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, promptWritten, "external cancellation prompt")
	if active := <-confirmationDone; !active {
		t.Fatal("external cancellation was not detected")
	}

	got := output.String()
	progressIndex := strings.Index(got, "[#########-----------] 43.8%")
	promptIndex := strings.Index(got, "[ChannelTerm] Cancel file transfer? [y/N]:")
	if progressIndex < 0 || promptIndex < 0 || progressIndex > promptIndex {
		t.Fatalf("output = %q, want prepared progress before cancellation prompt", got)
	}
	dispatcher.abandonExternalTransferCancellation()
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
		"File transfer started: sources.bin -> /tmp/cterm/user-files/sources.bin",
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
	for _, want := range []string{"Select: s\r\nLocal path: a.bin\r\n", "Remote path [/tmp/cterm/user-files/a.bin]:", "File transfer cancelled"} {
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
		{name: "send", input: []byte("\x1dfsa.bin\n\x1b"), want: "Select: s\r\nLocal path: a.bin\r\nRemote path [/tmp/cterm/user-files/a.bin]:"},
		{name: "receive", input: []byte("\x1dfr/var/log/app.log\n\x1b"), want: "Select: r\r\nRemote path: /var/log/app.log\r\nLocal path [" + defaultLocalTransferPath("/var/log/app.log") + "]:"},
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

func TestAttachInputDispatcherIgnoresInputDuringTerminalCommand(t *testing.T) {
	attached := &activeTerminalCommandAttachSession{fakeAttachSession: &fakeAttachSession{}, active: true}
	var output bytes.Buffer
	dispatcher := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, attached, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, func() error { return nil }, func() {}, nil)
	if !dispatcher.dispatch([]byte("ls\r")) {
		t.Fatal("dispatch stopped while ignoring terminal command input")
	}
	if got := attached.writtenData(); len(got) != 0 {
		t.Fatalf("remote input = %q, want none while terminal command is active", got)
	}
	if count := strings.Count(output.String(), "Input ignored while an AI command is running"); count != 1 {
		t.Fatalf("local warning count = %d, want one; output = %q", count, output.String())
	}
	if !dispatcher.dispatch([]byte("pwd\r")) {
		t.Fatal("second ignored dispatch stopped")
	}
	if count := strings.Count(output.String(), "Input ignored while an AI command is running"); count != 1 {
		t.Fatalf("repeated local warning count = %d, want one", count)
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

type presentationAwareExternalTransferSession struct {
	externalTransferAttachSession
	presentation *fileTransferPresentation
}

type activeTerminalCommandAttachSession struct {
	*fakeAttachSession
	active bool
}

func (s *activeTerminalCommandAttachSession) terminalCommandActive() bool { return s.active }

func (s *presentationAwareExternalTransferSession) fileTransferCancelPromptStarted() {
	s.presentation.beginCancelConfirmation()
}

func (s *presentationAwareExternalTransferSession) fileTransferCancelPromptFinished() {
	s.presentation.finishCancelConfirmation()
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
