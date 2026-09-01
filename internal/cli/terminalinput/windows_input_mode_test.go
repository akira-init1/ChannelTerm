package terminalinput

import "testing"

func TestWindowsRawInputMode(t *testing.T) {
	tests := []struct {
		name string
		mode uint32
		want uint32
	}{
		{
			name: "clears local input processing and enables VT input",
			mode: windowsEnableProcessedInput | windowsEnableLineInput | windowsEnableEchoInput | 0x0018,
			want: 0x0018 | windowsEnableVirtualTerminalInput,
		},
		{
			name: "preserves unrelated flags and existing VT input",
			mode: windowsEnableVirtualTerminalInput | 0x0080 | windowsEnableProcessedInput,
			want: windowsEnableVirtualTerminalInput | 0x0080,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := windowsRawInputMode(tt.mode); got != tt.want {
				t.Errorf("windowsRawInputMode(%#x) = %#x, want %#x", tt.mode, got, tt.want)
			}
		})
	}
}
