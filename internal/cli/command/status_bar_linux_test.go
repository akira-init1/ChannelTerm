//go:build linux

package command

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestStatusBarLinuxConsole(t *testing.T) {
	if os.Getenv("CHANNELTERM_TEST_STATUS_CONSOLE") != "1" {
		t.Skip("opt-in native Linux PTY test")
	}
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	if err = unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	if err = unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 80}); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	sample := []byte("abc\x1b[D中文\r")
	command := exec.Command(executable, "-test.run=^TestStatusBarNativeConsole$", "-test.v")
	command.Env = append(os.Environ(), "TERM=xterm-256color", "CHANNELTERM_TEST_STATUS_INPUT="+base64.StdEncoding.EncodeToString(sample))
	command.Stdin = slave
	command.Stdout = slave
	command.Stderr = slave
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() { waitErr = command.Wait(); close(done) }()
	capture := &statusConsoleCapture{ready: make(chan struct{})}
	drained := make(chan struct{})
	go func() { defer close(drained); _, _ = io.Copy(capture, master) }()
	defer func() { command.Process.Kill(); <-done; slave.Close(); master.Close(); <-drained }()
	select {
	case <-capture.ready:
	case <-time.After(10 * time.Second):
		t.Fatal("probe not ready: " + capture.snapshot())
	}
	if _, err = master.Write(sample[:3]); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err = master.Write(append(sample[3:], 4)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		if waitErr != nil {
			t.Fatal(waitErr, capture.snapshot())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("probe did not exit")
	}
}
