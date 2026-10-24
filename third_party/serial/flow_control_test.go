package serial

import "testing"

func TestFlowControlRejectsInvalidBeforeOpening(t *testing.T) {
	for _, mode := range []*Mode{nil, {FlowControl: FlowControl(255)}} {
		if port, err := Open("not-a-real-port", mode); err == nil || port != nil {
			t.Fatalf("invalid mode: port=%v err=%v", port, err)
		}
	}
}
