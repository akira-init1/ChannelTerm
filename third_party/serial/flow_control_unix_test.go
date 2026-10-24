//go:build linux || darwin || freebsd || openbsd

package serial

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestFlowControlTermiosTransitions(t *testing.T) {
	settings := &unix.Termios{}
	settings.Cflag = unix.CLOCAL | unix.CREAD
	settings.Iflag = unix.IXANY
	for _, flow := range []FlowControl{SoftwareFlowControl, HardwareFlowControl, NoFlowControl, SoftwareFlowControl, NoFlowControl} {
		setTermSettingsFlowControl(flow, settings)
		software := settings.Iflag & (unix.IXON | unix.IXOFF)
		if (flow == SoftwareFlowControl && software != unix.IXON|unix.IXOFF) || (flow != SoftwareFlowControl && software != 0) {
			t.Fatalf("flow=%v iflag=%x", flow, settings.Iflag)
		}
		if settings.Iflag&unix.IXANY != 0 || (settings.Cflag&tcCRTSCTS != 0) != (flow == HardwareFlowControl) {
			t.Fatalf("flow=%v settings=%+v", flow, settings)
		}
		if settings.Cflag&(unix.CLOCAL|unix.CREAD) != unix.CLOCAL|unix.CREAD {
			t.Fatal("unrelated flags changed")
		}
		if flow == SoftwareFlowControl && (settings.Cc[unix.VSTART] != 0x11 || settings.Cc[unix.VSTOP] != 0x13) {
			t.Fatal("wrong XON/XOFF bytes")
		}
	}
}
