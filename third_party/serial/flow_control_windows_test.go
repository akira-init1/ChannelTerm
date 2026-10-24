package serial

import "testing"

func TestFlowControlDCBTransitions(t *testing.T) {
	params := &dcb{Flags: dcbDTRControlEnable | dcbRTSControlEnable}
	for _, flow := range []FlowControl{SoftwareFlowControl, HardwareFlowControl, NoFlowControl, SoftwareFlowControl, NoFlowControl} {
		setDCBFlowControl(flow, params)
		software := params.Flags & (dcbInX | dcbOutX)
		if (flow == SoftwareFlowControl && software != dcbInX|dcbOutX) || (flow != SoftwareFlowControl && software != 0) {
			t.Fatalf("flow=%v flags=%x", flow, params.Flags)
		}
		if (params.Flags&dcbOutXCTSFlow != 0) != (flow == HardwareFlowControl) {
			t.Fatal("wrong CTS gating")
		}
		rts := params.Flags & ^dcbRTSControlDisbaleMask
		if (flow == HardwareFlowControl && rts != dcbRTSControlHandshake) || (flow != HardwareFlowControl && rts != dcbRTSControlEnable) {
			t.Fatalf("wrong RTS mode: %x", rts)
		}
		if params.Flags&dcbDTRControlEnable == 0 {
			t.Fatal("DTR changed")
		}
		if flow == SoftwareFlowControl && (params.XonChar != 0x11 || params.XoffChar != 0x13 || params.Flags&dcbTXContinueOnXOFF != 0) {
			t.Fatal("wrong software handshake")
		}
	}
}
