package interactive

import (
	"bytes"
	"testing"
)

// TestControllerProcessesTerminalInput verifies that each escape action keeps
// its ordering boundary relative to ordinary remote terminal bytes.
func TestControllerProcessesTerminalInput(t *testing.T) {
	tests := []struct {
		name       string
		input      []byte
		want       []Action
		escapeByte byte
	}{
		{name: "ordinary input", input: []byte("echo ok\r"), want: []Action{{Kind: ActionRemote, Data: []byte("echo ok\r")}}},
		{name: "control C remains remote", input: []byte{0x03}, want: []Action{{Kind: ActionRemote, Data: []byte{0x03}}}},
		{name: "quit", input: []byte{0x1D, 'q'}, want: []Action{{Kind: ActionQuit}}},
		{name: "help", input: []byte{0x1D, '?'}, want: []Action{{Kind: ActionHelp}}},
		{name: "literal escape", input: []byte{0x1D, ']'}, want: []Action{{Kind: ActionRemote, Data: []byte{0x1D}}}},
		{name: "unknown escape", input: []byte{0x1D, 'x', 'a'}, want: []Action{{Kind: ActionUnknownEscape, Command: 'x'}, {Kind: ActionRemote, Data: []byte("a")}}},
		{name: "custom escape byte", escapeByte: '~', input: []byte{'~', ']'}, want: []Action{{Kind: ActionRemote, Data: []byte{'~'}}}},
		{name: "mixed input keeps order", input: []byte{'a', 0x03, 0x1D, '?', 'b', 0x1D, ']', 'c'}, want: []Action{{Kind: ActionRemote, Data: []byte{'a', 0x03}}, {Kind: ActionHelp}, {Kind: ActionRemote, Data: []byte{'b'}}, {Kind: ActionRemote, Data: []byte{0x1D}}, {Kind: ActionRemote, Data: []byte{'c'}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := NewController(tt.escapeByte)
			got := controller.Process(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("Process(%x) returned %d actions, want %d: %#v", tt.input, len(got), len(tt.want), got)
			}
			for index := range tt.want {
				if got[index].Kind != tt.want[index].Kind || got[index].Command != tt.want[index].Command || !bytes.Equal(got[index].Data, tt.want[index].Data) {
					t.Errorf("action %d = %#v, want %#v", index, got[index], tt.want[index])
				}
			}
		})
	}
}

// TestControllerRetainsEscapeAcrossReads verifies that a terminal reader may
// split the escape prefix and its command into separate Read calls.
func TestControllerRetainsEscapeAcrossReads(t *testing.T) {
	controller := NewController(DefaultEscapeByte)
	if got := controller.Process([]byte{DefaultEscapeByte}); len(got) != 0 {
		t.Fatalf("Process(prefix) = %#v, want no action until command arrives", got)
	}
	got := controller.Process([]byte{'?'})
	if len(got) != 1 || got[0].Kind != ActionHelp {
		t.Errorf("Process(help command) = %#v, want ActionHelp", got)
	}
	got = controller.Process([]byte("next"))
	if len(got) != 1 || got[0].Kind != ActionRemote || !bytes.Equal(got[0].Data, []byte("next")) {
		t.Errorf("Process(normal after help) = %#v, want normal remote input", got)
	}
}
