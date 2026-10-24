// ChannelTerm modification to go.bug.st/serial v1.6.2; see CHANNELTERM.md.
package serial

import "errors"

// FlowControl selects native driver flow control without changing stream I/O.
type FlowControl uint8

const (
	// NoFlowControl disables software and hardware flow control (the default).
	NoFlowControl FlowControl = iota
	// SoftwareFlowControl enables XON/XOFF in both directions using DC1/DC3.
	SoftwareFlowControl
	// HardwareFlowControl enables CTS-gated writes and RTS receive handshaking.
	HardwareFlowControl
)

func validateFlowControl(mode *Mode) error {
	if mode == nil || mode.FlowControl > HardwareFlowControl {
		return errors.New("invalid serial flow control mode")
	}
	return nil
}
