//go:build windows

package command

import (
	"encoding/base64"
	"io"
	"os"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestStatusBarWindowsConsole(t *testing.T) {
	if os.Getenv("CHANNELTERM_TEST_STATUS_CONSOLE") != "1" {
		t.Skip("opt-in native ConPTY test")
	}
	sample := []byte("abc\x1b[D中文\r")
	t.Setenv("CHANNELTERM_TEST_STATUS_INPUT", base64.StdEncoding.EncodeToString(sample))
	t.Setenv("TERM", "xterm-256color")
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inR.Close()
	defer inW.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outR.Close()
	defer outW.Close()
	var console windows.Handle
	if err = windows.CreatePseudoConsole(windows.Coord{X: 80, Y: 24}, windows.Handle(inR.Fd()), windows.Handle(outW.Fd()), 0, &console); err != nil {
		t.Fatal(err)
	}
	capture := &statusConsoleCapture{ready: make(chan struct{})}
	drained := make(chan struct{})
	go func() { defer close(drained); _, _ = io.Copy(capture, outR) }()
	defer func() { windows.ClosePseudoConsole(console); outW.Close(); <-drained }()
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		t.Fatal(err)
	}
	defer attributes.Delete()
	// Win32 takes the opaque HPCON value itself, not an address to a HANDLE.
	// Reinterpret that foreign pointer representation without dereferencing it.
	value := *(*unsafe.Pointer)(unsafe.Pointer(&console))
	if err = attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, value, unsafe.Sizeof(console)); err != nil {
		t.Fatal(err)
	}
	startup := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{}))}, ProcThreadAttributeList: attributes.List()}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command, err := windows.UTF16PtrFromString(windows.ComposeCommandLine([]string{executable, "-test.run=^TestStatusBarNativeConsole$", "-test.v"}))
	if err != nil {
		t.Fatal(err)
	}
	var process windows.ProcessInformation
	if err = windows.CreateProcess(nil, command, nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT, nil, nil, &startup.StartupInfo, &process); err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process.Thread)
	defer windows.CloseHandle(process.Process)
	defer windows.TerminateProcess(process.Process, 1)
	select {
	case <-capture.ready:
	case <-time.After(10 * time.Second):
		t.Fatal("probe not ready: " + capture.snapshot())
	}
	// Allow an independent status refresh while a partially typed line is present.
	if _, err = inW.Write(sample[:3]); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err = inW.Write(append(sample[3:], 4)); err != nil {
		t.Fatal(err)
	}
	result, err := windows.WaitForSingleObject(process.Process, 10000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		t.Fatal("probe did not exit", result, err)
	}
	var code uint32
	_ = windows.GetExitCodeProcess(process.Process, &code)
	if code != 0 {
		t.Fatalf("probe exit %d: %s", code, capture.snapshot())
	}
	if strings.Contains(capture.snapshot(), "input mismatch") {
		t.Fatal(capture.snapshot())
	}
}

// A Go test launched from redirected CI may inherit redirected standard handles
// even when attached to ConPTY. Open only this test process's assigned console.
func statusConsoleFiles(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	in, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { out.Close() })
	return in, out
}
