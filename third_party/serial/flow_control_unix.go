//go:build linux || darwin || freebsd || openbsd

// ChannelTerm modification to go.bug.st/serial v1.6.2; see CHANNELTERM.md.
package serial

import "golang.org/x/sys/unix"

// Set both directions explicitly so a reused OS configuration cannot leak a
// previous flow mode. IXANY stays off: only XON may restart a stopped sender.
func setTermSettingsFlowControl(flow FlowControl, settings *unix.Termios) {
	settings.Iflag &^= unix.IXON | unix.IXOFF | unix.IXANY
	setTermSettingsCtsRts(flow == HardwareFlowControl, settings)
	if flow == SoftwareFlowControl {
		settings.Iflag |= unix.IXON | unix.IXOFF
		settings.Cc[unix.VSTART] = 0x11
		settings.Cc[unix.VSTOP] = 0x13
	}
}
