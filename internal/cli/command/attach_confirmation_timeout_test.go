package command

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

func TestExternalConfirmationTimeoutInvalidatesPromptAndConsumesLateAnswer(t *testing.T) {
	for _, result := range []string{"resumed", "completed", "invalid", "closed"} {
		t.Run(result, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				attached := &timeoutConfirmationSession{fakeAttachSession: &fakeAttachSession{}, state: result}
				var output bytes.Buffer
				input := make(chan attachInputResult)
				pump := &attachInputPump{results: input}
				d := newAttachInputDispatcherWithPump(ctx, pump, attached, func(p []byte) error { _, err := output.Write(p); return err }, nil, cancel, nil)
				if !d.beginExternalTransferCancellationConfirmation() {
					t.Fatal("confirmation did not begin")
				}
				done := make(chan struct{})
				go func() { d.run(); close(done) }()
				synctest.Wait()
				time.Sleep(app.FileTransferCancelConfirmationTimeout)
				synctest.Wait()
				if d.externalCancelRequest != "" || d.externalCancelTimer != nil {
					t.Fatal("expired local request still active")
				}
				if attached.calls != 1 || attached.confirmed {
					t.Fatal("local timeout confirmed cancellation")
				}
				if !strings.Contains(output.String(), "confirmation window ended") || strings.Contains(output.String(), "File transfer resumed") {
					t.Fatalf("misleading timeout output: %q", output.String())
				}
				if result == "completed" && !strings.Contains(output.String(), "already completed") {
					t.Fatal("completed status omitted")
				}
				if (result == "invalid" || result == "closed") && !strings.Contains(output.String(), "status is unconfirmed") {
					t.Fatal("unknown status presented as success")
				}
				input <- attachInputResult{data: []byte("y\r\n")}
				synctest.Wait()
				if attached.calls != 1 || len(attached.writtenData()) != 0 {
					t.Fatal("late answer sent as cancellation or terminal input")
				}
				if d.mode != attachInputModeAttach {
					t.Fatal("late answer did not dismiss notice")
				}
				cancel()
				<-done
			})
		})
	}
}

func TestExternalConfirmationChecksDeadlineBeforeTimerDispatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attached := &timeoutConfirmationSession{fakeAttachSession: &fakeAttachSession{}, state: "resumed"}
		d := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, attached, func([]byte) error { return nil }, nil, func() {}, nil)
		d.beginExternalTransferCancellationConfirmation()
		time.Sleep(app.FileTransferCancelConfirmationTimeout)
		d.resolveExternalTransferCancellationConfirmation(true)
		if attached.confirmed || attached.calls != 1 || d.externalCancelTimer != nil {
			t.Fatal("late answer bypassed presentation deadline")
		}
		if d.mode != attachInputModeFileTransferCancelConfirm {
			t.Fatal("timeout notice no longer consumes its dismiss key")
		}
		d.dispatch([]byte("y\r\n"))
		if attached.calls != 1 || len(attached.writtenData()) != 0 || d.mode != attachInputModeAttach {
			t.Fatal("notice dismiss key escaped into cancellation or device input")
		}
	})
}

func TestExternalConfirmationFinishesTimerOnAnswerOrDetach(t *testing.T) {
	for _, end := range []string{"answer", "detach", "host-expired"} {
		t.Run(end, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				attached := &timeoutConfirmationSession{fakeAttachSession: &fakeAttachSession{}, state: "resumed"}
				var output bytes.Buffer
				d := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, attached, func(p []byte) error { _, err := output.Write(p); return err }, nil, func() {}, nil)
				d.beginExternalTransferCancellationConfirmation()
				if end == "detach" {
					d.abandonExternalTransferCancellation()
				} else {
					if end == "host-expired" {
						attached.state = "invalid"
					}
					d.resolveExternalTransferCancellationConfirmation(false)
				}
				if d.externalCancelTimer != nil || d.externalCancelRequest != "" {
					t.Fatal("finished prompt retained timer/request")
				}
				if end == "host-expired" && (!strings.Contains(output.String(), "answer could not be confirmed") || strings.Contains(output.String(), "File transfer resumed")) {
					t.Fatal("rejected answer presented as resumed")
				}
			})
		})
	}
}

type timeoutConfirmationSession struct {
	*fakeAttachSession
	state     string
	calls     int
	confirmed bool
}

func (*timeoutConfirmationSession) BeginFileTransferCancel(context.Context) (string, bool, error) {
	return "request", true, nil
}

func (s *timeoutConfirmationSession) ResolveFileTransferCancel(_ context.Context, requestID string, cancel bool) (string, error) {
	s.calls++
	s.confirmed = s.confirmed || cancel
	if requestID != "request" {
		return "", errors.New("unexpected request")
	}
	if s.state == "invalid" {
		return "", app.ErrFileTransferCancelRequest
	}
	if s.state == "closed" {
		return "", app.ErrSessionNotFound
	}
	return s.state, nil
}
