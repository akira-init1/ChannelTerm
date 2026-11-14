package command

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

type controlAttachFixture struct {
	fakeAttachSession
	status        app.ControlStatus
	action        func(attachControlRequest) (attachControlReply, error)
	commandActive bool
}

func (f *controlAttachFixture) controlStatus(context.Context) (app.ControlStatus, error) {
	return f.status, nil
}
func (f *controlAttachFixture) controlAction(_ context.Context, q attachControlRequest) (attachControlReply, error) {
	return f.action(q)
}
func (f *controlAttachFixture) terminalCommandActive() bool { return f.commandActive }

func TestAttachControlPinsConfirmedTargetAndDiscardsInput(t *testing.T) {
	old := app.ControlStatus{SessionID: "original", Version: "v1", OperationID: "operation1", Writable: true}
	f := &controlAttachFixture{status: old, commandActive: true}
	calls := make(chan attachControlRequest, 8)
	release := make(chan struct{})
	f.action = func(q attachControlRequest) (attachControlReply, error) {
		calls <- q
		<-release
		return attachControlReply{result: app.ControlResult{SessionID: q.target.SessionID, Version: q.target.Version, OperationID: q.target.OperationID, Status: "taken_over", Recovery: "restored"}}, nil
	}
	var out bytes.Buffer
	d := newAttachInputDispatcherWithPump(context.Background(), nil, f, func(p []byte) error { _, err := out.Write(p); return err }, nil, func() {}, nil)
	d.dispatch([]byte("ignored\x1diYpasted"))
	if d.mode != attachInputModeControlConfirm || !strings.Contains(out.String(), "original") {
		t.Fatal(out.String())
	}
	// Operation completion/selection changes after confirmation opens must not retarget.
	f.status = app.ControlStatus{SessionID: "successor", Version: "v2", OperationID: "operation2", Writable: false}
	d.dispatch([]byte("yignored"))
	q := <-calls
	if q.target != old || q.action != "takeover" {
		t.Fatalf("retargeted: %+v", q)
	}
	d.dispatch([]byte("pending-input\x1di"))
	close(release)
	select {
	case reply := <-d.controlDone:
		d.finishControlRequest(reply)
	case <-time.After(time.Second):
		t.Fatal("worker did not finish")
	}
	if !d.controlInputPaused {
		t.Fatal("read-only observation enabled input")
	}
	d.dispatch([]byte("still-ignored"))
	d.dispatch([]byte("\x1dr"))
	q = <-calls
	if q.target != old || q.action != "result" {
		t.Fatalf("query retargeted: %+v", q)
	}
	d.finishControlRequest(<-d.controlDone)
	if got := f.writtenData(); len(got) != 0 {
		t.Fatalf("shortcut/paste reached remote: %q", got)
	}
}

func TestAttachControlDenialKeepsInputPausedAndAllowIsExplicit(t *testing.T) {
	f := &controlAttachFixture{status: app.ControlStatus{SessionID: "s", Version: "v", Writable: true}}
	count := 0
	f.action = func(q attachControlRequest) (attachControlReply, error) {
		count++
		if q.action == "allow" {
			return attachControlReply{control: f.status}, nil
		}
		return attachControlReply{}, errors.New("permission denied")
	}
	var out bytes.Buffer
	d := newAttachInputDispatcherWithPump(context.Background(), nil, f, func(p []byte) error { _, err := out.Write(p); return err }, nil, func() {}, nil)
	d.dispatch([]byte("\x1di"))
	d.dispatch([]byte("nnever-send"))
	if count != 0 {
		t.Fatal("dismissed request sent")
	}
	d.dispatch([]byte("\x1di"))
	d.dispatch([]byte("y"))
	d.finishControlRequest(<-d.controlDone)
	d.dispatch([]byte("stale-input"))
	if !d.controlInputPaused || len(f.writtenData()) != 0 {
		t.Fatal("denial replayed input")
	}
	d.dispatch([]byte("\x1da"))
	d.dispatch([]byte("yold-tail"))
	d.finishControlRequest(<-d.controlDone)
	if count != 2 || d.controlInputPaused {
		t.Fatal("explicit allow failed")
	}
	d.dispatch([]byte("new"))
	if got := string(f.writtenData()); got != "new" {
		t.Fatalf("input = %q", got)
	}
}
