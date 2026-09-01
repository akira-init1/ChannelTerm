package terminalinput

const (
	windowsKeyEvent                   uint16 = 0x0001
	windowsEnableProcessedInput       uint32 = 0x0001
	windowsEnableLineInput            uint32 = 0x0002
	windowsEnableEchoInput            uint32 = 0x0004
	windowsEnableVirtualTerminalInput uint32 = 0x0200
	windowsVirtualKeyShift            uint16 = 0x10
	windowsVirtualKeyControl          uint16 = 0x11
	windowsVirtualKeyMenu             uint16 = 0x12
	windowsVirtualKeyEscape           uint16 = 0x1B
	windowsRightAltPressed            uint32 = 0x0001
	windowsLeftAltPressed             uint32 = 0x0002
)

// windowsKeyEventRecord mirrors the KEY_EVENT_RECORD payload at the start of
// the INPUT_RECORD event union. Keeping the pure layout and planning logic in
// this platform-neutral file lets every host test the Windows input contract.
type windowsKeyEventRecord struct {
	keyDown         int32
	repeatCount     uint16
	virtualKeyCode  uint16
	virtualScanCode uint16
	unicodeChar     uint16
	controlKeyState uint32
}

// windowsInputRecord mirrors the fixed-size Win32 INPUT_RECORD layout. Only
// key events are inspected; other event payloads remain opaque padding.
type windowsInputRecord struct {
	eventType uint16
	_         uint16
	key       windowsKeyEventRecord
}

type windowsEscapeReadPlan struct {
	discardRecords  int
	escapeRepeats   int
	readSingleUTF16 bool
}

// windowsRawInputMode derives the console input mode needed to receive raw
// terminal bytes while preserving every unrelated console-mode setting.
func windowsRawInputMode(mode uint32) uint32 {
	mode &^= windowsEnableEchoInput | windowsEnableLineInput | windowsEnableProcessedInput
	return mode | windowsEnableVirtualTerminalInput
}

// windowsEscapePlan identifies a queued Esc key before ReadConsole translates
// it. Key-up, modifier, and non-key records may be consumed because the normal
// byte-stream read discards them. If a character-producing non-Alt key precedes
// Esc, the caller reads one UTF-16 value first and replans so input order
// remains unchanged.
func windowsEscapePlan(records []windowsInputRecord) windowsEscapeReadPlan {
	ignorable := 0
	producerSeen := false
	producerCanReadAlone := false
	for index, record := range records {
		if record.eventType != windowsKeyEvent || record.key.keyDown == 0 || windowsModifierKey(record.key.virtualKeyCode) {
			if !producerSeen {
				ignorable = index + 1
			}
			continue
		}
		if record.key.virtualKeyCode == windowsVirtualKeyEscape || record.key.unicodeChar == 0x1B {
			if producerSeen {
				return windowsEscapeReadPlan{readSingleUTF16: producerCanReadAlone}
			}
			repeats := int(record.key.repeatCount)
			if repeats == 0 {
				repeats = 1
			}
			return windowsEscapeReadPlan{discardRecords: index + 1, escapeRepeats: repeats}
		}
		if !producerSeen {
			producerSeen = true
			producerCanReadAlone = record.key.unicodeChar != 0 && record.key.controlKeyState&(windowsRightAltPressed|windowsLeftAltPressed) == 0
		}
	}
	if !producerSeen {
		return windowsEscapeReadPlan{discardRecords: ignorable}
	}
	return windowsEscapeReadPlan{}
}

func windowsModifierKey(virtualKeyCode uint16) bool {
	return virtualKeyCode == windowsVirtualKeyShift || virtualKeyCode == windowsVirtualKeyControl || virtualKeyCode == windowsVirtualKeyMenu
}
