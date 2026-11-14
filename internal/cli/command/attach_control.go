package command

import (
	"context"
	"fmt"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/interactive"
	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

// attachControlClient exposes only exact-target control operations to local input.
type attachControlClient interface {
	controlStatus(context.Context) (app.ControlStatus, error)
	controlAction(context.Context, attachControlRequest) (attachControlReply, error)
}
type attachControlRequest struct {
	action string
	target app.ControlStatus
}
type attachControlReply struct {
	result  app.ControlResult
	control app.ControlStatus
	err     error
}

func (s *mcpAttachSession) controlStatus(ctx context.Context) (app.ControlStatus, error) {
	var out struct {
		Control app.ControlStatus `json:"control"`
	}
	c, ok := s.client.(*instanceMCPClient)
	if !ok || !c.controls.available {
		return out.Control, app.ErrControlRequired
	}
	err := c.controlCall(ctx, map[string]any{"action": "status", "session_id": s.id}, &out)
	return out.Control, err
}
func (s *mcpAttachSession) controlAction(ctx context.Context, request attachControlRequest) (attachControlReply, error) {
	var out struct {
		Control app.ControlStatus `json:"control"`
		Result  app.ControlResult `json:"result"`
	}
	c, ok := s.client.(*instanceMCPClient)
	if !ok || !c.controls.available || request.target.SessionID != s.id {
		return attachControlReply{}, app.ErrControlRequired
	}
	err := c.controlCall(ctx, map[string]any{"action": request.action, "session_id": request.target.SessionID, "version": request.target.Version, "operation_id": request.target.OperationID}, &out)
	if err == nil {
		if request.action == "allow" {
			if out.Control.SessionID != request.target.SessionID {
				err = app.ErrControlStale
			}
		} else if out.Result.SessionID != request.target.SessionID || out.Result.Version != request.target.Version || out.Result.OperationID != request.target.OperationID {
			err = app.ErrControlStale
		}
	}
	return attachControlReply{result: out.Result, control: out.Control}, err
}

func (d *attachInputDispatcher) controlNotice(message string) {
	if d.writeLocal != nil {
		_ = d.writeLocal([]byte("\r\n[ChannelTerm] " + message + "\r\n"))
	}
}
func (d *attachInputDispatcher) discardControlInput() {
	d.discardBefore = time.Now()
	if d.pump != nil {
		d.pump.mu.Lock()
		d.pump.pending = nil
		d.pump.mu.Unlock()
	}
}
func (d *attachInputDispatcher) beginControlRequest(kind interactive.ActionKind) {
	client, ok := d.terminal.(attachControlClient)
	if !ok {
		d.controlNotice("Shared control is unavailable.")
		return
	}
	if kind == interactive.ActionControlResult {
		if d.controlLast == nil {
			d.controlNotice("No captured takeover result to query.")
			return
		}
		request := *d.controlLast
		request.action = "result"
		d.startControlRequest(client, request)
		return
	}
	ctx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
	defer cancel()
	target, err := client.controlStatus(ctx)
	d.discardControlInput()
	if err != nil {
		d.controlNotice("Control status unavailable: " + err.Error())
		return
	}
	action := "takeover"
	warning := "Stop this captured operation and take control? Recovery may close the Session; remote background work may continue."
	if kind == interactive.ActionAllowAI {
		action = "allow"
		warning = "Allow new AI operations? Cancelled work and discarded input will not restart."
	}
	d.controlDraft = &attachControlRequest{action: action, target: target}
	d.mode = attachInputModeControlConfirm
	d.controlNotice(fmt.Sprintf("%s\r\nSession %s · operation %s · version %s\r\nConfirm [y/N]:", warning, target.SessionID, target.OperationID, target.Version))
}
func (d *attachInputDispatcher) confirmControlRequest(yes bool) {
	draft := d.controlDraft
	d.controlDraft = nil
	d.mode = attachInputModeAttach
	d.discardControlInput()
	if !yes || draft == nil {
		d.controlNotice("Control request dismissed.")
		return
	}
	client, ok := d.terminal.(attachControlClient)
	if !ok {
		return
	}
	d.startControlRequest(client, *draft)
}

// One bounded worker only queries the captured key after takeover. It never
// repeats a mutation on timeout and cannot follow a successor operation.
func (d *attachInputDispatcher) startControlRequest(client attachControlClient, request attachControlRequest) {
	if request.action != "allow" {
		saved := request
		d.controlLast = &saved
	}
	d.controlInputPaused = true
	d.mode = attachInputModeControlWait
	done := make(chan attachControlReply, 1)
	d.controlDone = done
	d.controlNotice("Control request pending; input remains paused. Ctrl+] q detaches.")
	go func() {
		ctx, cancel := context.WithTimeout(d.ctx, 12*time.Second)
		defer cancel()
		reply, err := client.controlAction(ctx, request)
		for err == nil && reply.result.Status == "stopping" {
			timer := time.NewTimer(200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				err = ctx.Err()
			case <-timer.C:
			}
			if err != nil {
				break
			}
			request.action = "result"
			reply, err = client.controlAction(ctx, request)
		}
		reply.err = err
		done <- reply
	}()
}
func (d *attachInputDispatcher) finishControlRequest(reply attachControlReply) {
	d.controlDone = nil
	d.mode = attachInputModeAttach
	d.discardControlInput()
	if reply.err != nil {
		d.controlNotice("Control result unconfirmed or request rejected: " + reply.err.Error() + ". Do not retry input. Ctrl+] r queries the original takeover.")
		return
	}
	if reply.control.SessionID != "" {
		d.confirmControlWritable()
		d.controlNotice("AI pause removed; no work restarted.")
		return
	}
	switch reply.result.Status {
	case "taken_over":
		d.confirmControlWritable()
		d.controlNotice("Recovery confirmed; AI remains paused. Current Host authority determines input access.")
	case "failed":
		d.controlNotice("Recovery failed; original Session closed. Remote background work may continue.")
	default:
		d.controlNotice("Recovery pending or unknown; keep input paused. Ctrl+] r queries the original result.")
	}
}

// confirmControlWritable is observation only; every subsequent write is still
// checked by Host. Failed observation keeps local input paused without replay.
func (d *attachInputDispatcher) confirmControlWritable() {
	if client, ok := d.terminal.(attachControlClient); ok {
		ctx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
		status, err := client.controlStatus(ctx)
		cancel()
		d.controlInputPaused = err != nil || !status.Writable
		d.discardControlInput()
	}
}
