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

// TestWindowsEscapePlanPassesEscape verifies that the Windows input adapter
// turns an Esc key event into byte-oriented input even when key-up and console
// events remain queued before it.
func TestWindowsEscapePlanPassesEscape(t *testing.T) {
	records := []windowsInputRecord{
		{eventType: 0x0002},
		{eventType: windowsKeyEvent, key: windowsKeyEventRecord{virtualKeyCode: windowsVirtualKeyControl}},
		{eventType: windowsKeyEvent, key: windowsKeyEventRecord{keyDown: 1, virtualKeyCode: windowsVirtualKeyEscape}},
	}
	got := windowsEscapePlan(records)
	if got.discardRecords != len(records) || got.escapeRepeats != 1 || got.readSingleUTF16 {
		t.Errorf("windowsEscapePlan() = %#v, want discard %d and one Esc byte", got, len(records))
	}
}

// TestWindowsEscapePlanPreservesInputOrder verifies that an already-queued
// Ctrl+] byte is read before a following Esc key is normalized.
func TestWindowsEscapePlanPreservesInputOrder(t *testing.T) {
	records := []windowsInputRecord{
		{eventType: windowsKeyEvent, key: windowsKeyEventRecord{keyDown: 1, unicodeChar: 0x1D}},
		{eventType: windowsKeyEvent, key: windowsKeyEventRecord{virtualKeyCode: ']'}},
		{eventType: windowsKeyEvent, key: windowsKeyEventRecord{keyDown: 1, virtualKeyCode: windowsVirtualKeyEscape}},
	}
	if got := windowsEscapePlan(records); !got.readSingleUTF16 || got.discardRecords != 0 || got.escapeRepeats != 0 {
		t.Fatalf("windowsEscapePlan(prefix, Esc) = %#v, want one prefix byte read first", got)
	}
	got := windowsEscapePlan(records[1:])
	if got.discardRecords != 2 || got.escapeRepeats != 1 || got.readSingleUTF16 {
		t.Errorf("windowsEscapePlan(Esc suffix) = %#v, want queued Esc after key-up", got)
	}
}

// TestWindowsEscapePlanPassesControlBracketEscape verifies any Windows key
// event already translated to byte 0x1b follows the same Esc path.
func TestWindowsEscapePlanPassesControlBracketEscape(t *testing.T) {
	records := []windowsInputRecord{{
		eventType: windowsKeyEvent,
		key: windowsKeyEventRecord{
			keyDown:        1,
			virtualKeyCode: '[',
			unicodeChar:    0x1B,
		},
	}}
	got := windowsEscapePlan(records)
	if got.discardRecords != 1 || got.escapeRepeats != 1 {
		t.Errorf("windowsEscapePlan(Ctrl+[) = %#v, want one Esc byte", got)
	}
}
