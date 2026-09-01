package terminalinput

const (
	windowsEnableProcessedInput       uint32 = 0x0001
	windowsEnableLineInput            uint32 = 0x0002
	windowsEnableEchoInput            uint32 = 0x0004
	windowsEnableVirtualTerminalInput uint32 = 0x0200
)

// windowsRawInputMode derives the console input mode needed to receive raw
// terminal bytes while preserving every unrelated console-mode setting.
func windowsRawInputMode(mode uint32) uint32 {
	mode &^= windowsEnableEchoInput | windowsEnableLineInput | windowsEnableProcessedInput
	return mode | windowsEnableVirtualTerminalInput
}
