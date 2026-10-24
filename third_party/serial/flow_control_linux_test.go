package serial

import (
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A disposable PTY verifies real termios configuration and Close, without
// touching any discovered serial port or claiming electrical handshake tests.
func TestFlowControlPTYConfigurationAndClose(t *testing.T) {
	for _, flow := range []FlowControl{NoFlowControl, SoftwareFlowControl, HardwareFlowControl} {
		t.Run(fmt.Sprint(flow), func(t *testing.T) {
			master, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(master)
			if err := unix.IoctlSetPointerInt(master, unix.TIOCSPTLCK, 0); err != nil {
				t.Fatal(err)
			}
			n, err := unix.IoctlGetInt(master, unix.TIOCGPTN)
			if err != nil {
				t.Fatal(err)
			}
			opened, err := Open(fmt.Sprintf("/dev/pts/%d", n), &Mode{BaudRate: 115200, FlowControl: flow})
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()
			port := opened.(*unixPort)
			settings, err := port.getTermSettings()
			if err != nil {
				t.Fatal(err)
			}
			if (settings.Iflag&unix.IXON != 0) != (flow == SoftwareFlowControl) || (settings.Cflag&tcCRTSCTS != 0) != (flow == HardwareFlowControl) {
				t.Fatalf("unexpected settings: %+v", settings)
			}
			if flow == SoftwareFlowControl {
				// TCXONC stops local output deterministically, as an incoming XOFF
				// would, without a timing race between tty input and Write.
				if err := unix.IoctlSetInt(port.handle, unix.TCXONC, unix.TCOOFF); err != nil {
					t.Fatal(err)
				}
				writeDone := make(chan error, 1)
				go func() { _, err := opened.Write([]byte("paused")); writeDone <- err }()
				select {
				case <-writeDone:
					t.Fatal("output was not stopped")
				case <-time.After(30 * time.Millisecond):
				}
				if err := unix.IoctlSetInt(port.handle, unix.TCXONC, unix.TCOON); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-writeDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("output did not resume")
				}
				if err := unix.IoctlSetInt(port.handle, unix.TCXONC, unix.TCOOFF); err != nil {
					t.Fatal(err)
				}
				go func() { _, err := opened.Write([]byte("cancelled")); writeDone <- err }()
				select {
				case <-writeDone:
					t.Fatal("second write was not stopped")
				case <-time.After(30 * time.Millisecond):
				}
				closeDone := make(chan error, 1)
				go func() { closeDone <- opened.Close() }()
				select {
				case err := <-writeDone:
					if err == nil {
						t.Fatal("flow-stopped write unexpectedly succeeded during Close")
					}
				case <-time.After(time.Second):
					_, _ = unix.Write(master, []byte{0x11})
					t.Fatal("Close did not cancel flow-stopped write")
				}
				select {
				case err := <-closeDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("Close blocked")
				}
			}
			started, done := make(chan struct{}), make(chan struct{})
			go func() { close(started); _, _ = opened.Read(make([]byte, 1)); close(done) }()
			<-started
			if err := opened.Close(); err != nil {
				t.Fatal(err)
			}
			if err := opened.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("Close did not release read")
			}
		})
	}
}
