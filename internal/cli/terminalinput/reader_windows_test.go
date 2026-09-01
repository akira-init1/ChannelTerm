//go:build windows

package terminalinput

import (
	"errors"
	"testing"
	"unsafe"
)

// TestWindowsInputRecordLayout protects the Win32 INPUT_RECORD and
// KEY_EVENT_RECORD layouts used by PeekConsoleInputW and ReadConsoleInputW.
func TestWindowsInputRecordLayout(t *testing.T) {
	if got := unsafe.Sizeof(windowsKeyEventRecord{}); got != 16 {
		t.Errorf("KEY_EVENT_RECORD size = %d, want 16", got)
	}
	if got := unsafe.Sizeof(windowsInputRecord{}); got != 20 {
		t.Errorf("INPUT_RECORD size = %d, want 20", got)
	}
}

// TestWindowsConsoleReaderPassesEscapeByte verifies the Windows console input
// path emits byte 0x1b from VK_ESCAPE without falling through to ReadConsole.
func TestWindowsConsoleReaderPassesEscapeByte(t *testing.T) {
	events := &fakeWindowsConsoleEvents{records: []windowsInputRecord{
		{eventType: 0x0002},
		{eventType: windowsKeyEvent, key: windowsKeyEventRecord{keyDown: 1, virtualKeyCode: windowsVirtualKeyEscape}},
	}}
	reader := &windowsConsoleReader{events: events}
	buffer := make([]byte, 1)
	n, err := reader.Read(buffer)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 1 || buffer[0] != 0x1B {
		t.Errorf("Read() = %d, %x, want one Esc byte", n, buffer[:n])
	}
	if events.consumed != 2 {
		t.Errorf("consumed records = %d, want 2", events.consumed)
	}
}

type fakeWindowsConsoleEvents struct {
	records    []windowsInputRecord
	consumed   int
	characters []uint16
}

func (*fakeWindowsConsoleEvents) Wait() error {
	return nil
}

func (s *fakeWindowsConsoleEvents) Peek() ([]windowsInputRecord, error) {
	return append([]windowsInputRecord(nil), s.records...), nil
}

func (s *fakeWindowsConsoleEvents) Consume(count int) error {
	if count > len(s.records) {
		return errors.New("consume exceeds queued records")
	}
	s.consumed += count
	s.records = s.records[count:]
	return nil
}

func (s *fakeWindowsConsoleEvents) ReadUTF16(limit int) ([]uint16, error) {
	if len(s.characters) == 0 {
		return nil, errors.New("unexpected ReadUTF16 call")
	}
	for index, record := range s.records {
		if record.eventType == windowsKeyEvent && record.key.keyDown != 0 && !windowsModifierKey(record.key.virtualKeyCode) {
			s.records = s.records[index+1:]
			break
		}
	}
	count := min(limit, len(s.characters))
	result := append([]uint16(nil), s.characters[:count]...)
	s.characters = s.characters[count:]
	return result, nil
}

// TestWindowsConsoleReaderKeepsPrefixBeforeEscape verifies queued Ctrl+] and
// Esc records become ordered bytes across reads, allowing the controller to
// enter and then cancel local escape mode.
func TestWindowsConsoleReaderKeepsPrefixBeforeEscape(t *testing.T) {
	events := &fakeWindowsConsoleEvents{
		records: []windowsInputRecord{
			{eventType: windowsKeyEvent, key: windowsKeyEventRecord{keyDown: 1, unicodeChar: 0x1D}},
			{eventType: windowsKeyEvent, key: windowsKeyEventRecord{virtualKeyCode: ']'}},
			{eventType: windowsKeyEvent, key: windowsKeyEventRecord{keyDown: 1, virtualKeyCode: windowsVirtualKeyEscape}},
		},
		characters: []uint16{0x1D},
	}
	reader := &windowsConsoleReader{events: events}
	for index, want := range []byte{0x1D, 0x1B} {
		buffer := make([]byte, 1)
		n, err := reader.Read(buffer)
		if err != nil {
			t.Fatalf("Read() byte %d error = %v", index, err)
		}
		if n != 1 || buffer[0] != want {
			t.Fatalf("Read() byte %d = %d, %x, want %x", index, n, buffer[:n], want)
		}
	}
}

// TestWindowsConsoleReaderPreservesUTF8AcrossSmallReads verifies the Windows
// reader owns UTF-16 conversion buffering rather than hiding bytes behind a
// console-handle wait.
func TestWindowsConsoleReaderPreservesUTF8AcrossSmallReads(t *testing.T) {
	events := &fakeWindowsConsoleEvents{
		records:    []windowsInputRecord{{eventType: windowsKeyEvent, key: windowsKeyEventRecord{keyDown: 1, unicodeChar: '日'}}},
		characters: []uint16{'日'},
	}
	reader := &windowsConsoleReader{events: events}
	want := []byte("日")
	for index := range want {
		buffer := make([]byte, 1)
		n, err := reader.Read(buffer)
		if err != nil {
			t.Fatalf("Read() byte %d error = %v", index, err)
		}
		if n != 1 || buffer[0] != want[index] {
			t.Fatalf("Read() byte %d = %d, %x, want %x", index, n, buffer[:n], want[index])
		}
	}
}
