package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"golang.org/x/sys/unix"
)

type bridgePTYTransport struct{ master *os.File }

func (p bridgePTYTransport) Connect(context.Context) (channel.Channel, error) {
	return channel.NewStream(p.master)
}

// TestRSPBridgePTYBinaryAndShellRestoration uses a real Linux PTY with cat as a
// byte-stream simulator. It exercises neither GDB/RSP semantics nor hardware.
// The fixture, not ChannelTerm, prepares the target and brings back its shell.
func TestRSPBridgePTYBinaryAndShellRestoration(t *testing.T) {
	for _, name := range []string{"stty", "cat", "sh"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(err)
		}
	}
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(fd), "bridge-test-pty")
	defer master.Close()
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	original, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	stty := exec.Command("stty", "-g")
	stty.Stdin = slave
	saved, err := stty.Output()
	if err != nil {
		t.Fatal(err)
	}
	raw := exec.Command("stty", "raw", "-echo")
	raw.Stdin = slave
	if err := raw.Run(); err != nil {
		t.Fatal(err)
	}
	target := exec.Command("cat")
	target.Stdin, target.Stdout, target.Stderr = slave, slave, slave
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { target.Process.Kill(); target.Wait() }()
	s, err := session.New("bridge-serial", bridgePTYTransport{master})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	m := session.NewManager()
	defer m.Close()
	if err = m.RegisterWithMetadata(s, session.SessionMetadata{Transport: "serial", Endpoint: "PTY-SIMULATION"}); err != nil {
		t.Fatal(err)
	}
	a, err := New(Dependencies{Manager: m})
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.StartRSPBridge(context.Background(), bridgeRequest())
	if err != nil {
		t.Fatal(err)
	}
	defer a.CloseRSPBridge(result.Status.BridgeID, bridgeTestOwner)
	client, err := debugger.DialBridge(context.Background(), *result.Connection)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	payload := []byte{0, 0xff, '$', '}', '#', '+', '-', 3, '\r', '\n'}
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(client, got); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("PTY bytes: %x %v", got, err)
	}
	client.Close()
	waitBridgeState(t, a, result.Status.BridgeID, "disconnected")
	// Target preparation/recovery belongs to the external operator in this mode.
	target.Process.Kill()
	target.Wait()
	shell := exec.Command("sh", "-i")
	shell.Stdin, shell.Stdout, shell.Stderr = slave, slave, slave
	shell.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := shell.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { shell.Process.Kill(); shell.Wait() }()
	status, err := a.RestoreRSPBridge(result.Status.BridgeID, bridgeTestOwner, strings.TrimSpace(string(saved)), true)
	if err != nil || status.State != "restored" {
		t.Fatalf("PTY recovery: %+v %v", status, err)
	}
	restored, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || !reflect.DeepEqual(restored, original) {
		t.Fatalf("TTY differs after recovery: %v", err)
	}
}
