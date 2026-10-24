// ChannelTerm modification to go.bug.st/serial v1.6.2; see CHANNELTERM.md.
package serial

// Configure only flow-related DCB bits; DTR, framing, timeouts and overlapped
// read/write/close ownership remain upstream's responsibility.
func setDCBFlowControl(flow FlowControl, params *dcb) {
	params.Flags &^= dcbOutXCTSFlow | dcbInX | dcbOutX
	params.Flags |= dcbTXContinueOnXOFF
	if params.Flags & ^dcbRTSControlDisbaleMask == dcbRTSControlHandshake {
		params.Flags &= dcbRTSControlDisbaleMask
		params.Flags |= dcbRTSControlEnable
	}
	switch flow {
	case SoftwareFlowControl:
		params.Flags |= dcbInX | dcbOutX
		params.Flags &^= dcbTXContinueOnXOFF
		params.XonChar, params.XoffChar = 0x11, 0x13
		params.XonLim, params.XoffLim = 2048, 512
	case HardwareFlowControl:
		params.Flags &= dcbRTSControlDisbaleMask
		params.Flags |= dcbRTSControlHandshake | dcbOutXCTSFlow
	}
}
