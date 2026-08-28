package highlight

import (
	"bytes"
	"testing"
)

func TestRendererHighlightsNegativePhrasesAcrossChunks(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	if _, err := renderer.Write([]byte("driver is not op")); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	if output.Len() != 0 {
		t.Errorf("partial line output = %q, want no output before phrase completes", output.String())
	}
	if _, err := renderer.Write([]byte("en\n")); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	want := "driver is " + string(styleError) + "not open" + string(styleReset) + "\n"
	if got := output.String(); got != want {
		t.Errorf("rendered output = %q, want %q", got, want)
	}
}

func TestRendererHighlightsCRLFTerminatedLineAcrossChunks(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	if _, err := renderer.Write([]byte("driver is not open\r")); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	if output.Len() != 0 {
		t.Errorf("split CRLF output = %q, want buffered line", output.String())
	}
	if _, err := renderer.Write([]byte("\n")); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	want := "driver is " + string(styleError) + "not open" + string(styleReset) + "\r\n"
	if got := output.String(); got != want {
		t.Errorf("CRLF rendered output = %q, want %q", got, want)
	}
}

func TestRendererHighlightsCRLFTerminatedLines(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	if _, err := renderer.Write([]byte("ERROR: cannot connect: not open\r\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte(styleError+"ERROR"+styleReset)) {
		t.Errorf("output = %q, want ERROR styling for a CRLF line", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte(styleError+"cannot"+styleReset)) {
		t.Errorf("output = %q, want cannot styling for a CRLF line", output.String())
	}
	if !bytes.HasSuffix(output.Bytes(), []byte("\r\n")) {
		t.Errorf("output = %q, want original CRLF suffix", output.String())
	}
}

func TestRendererHighlightsCannotSpellings(t *testing.T) {
	tests := []string{
		"cannot connect\n",
		"can not connect\n",
		"can't connect\n",
		"can’t connect\n",
		"can n't connect\n",
		"can n’t connect\n",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			var output bytes.Buffer
			renderer := New(&output)
			if _, err := renderer.Write([]byte(input)); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if !bytes.Contains(output.Bytes(), []byte(styleError)) || !bytes.Contains(output.Bytes(), []byte(styleReset)) {
				t.Errorf("rendered output = %q, want error styling", output.String())
			}
		})
	}
}

func TestRendererStylesPromptBeforeNewlineAndPreservesEcho(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	if _, err := renderer.Write([]byte("root@boa")); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	if _, err := renderer.Write([]byte("rd:~# ")); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	wantPrompt := string(stylePromptUser) + "root" + string(styleReset) + "@" + string(stylePromptHost) + "board" + string(styleReset) + ":" + string(stylePromptPath) + "~" + string(styleReset) + string(stylePromptUser) + "#" + string(styleReset) + " "
	if got := output.String(); got != wantPrompt {
		t.Errorf("prompt = %q, want %q", got, wantPrompt)
	}
	if _, err := renderer.Write([]byte("ls")); err != nil {
		t.Fatalf("echo Write() error = %v", err)
	}
	if got := output.String(); got != wantPrompt+"ls" {
		t.Errorf("echo output = %q, want prompt followed by raw echo", got)
	}
	if _, err := renderer.Write([]byte("\r\n")); err != nil {
		t.Fatalf("echo terminator Write() error = %v", err)
	}
	if got := output.String(); got != wantPrompt+"ls\r\n" {
		t.Errorf("echo terminator output = %q, want raw carriage return and newline", got)
	}
}

func TestRendererUsesBrightPaletteStyles(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	if _, err := renderer.Write([]byte("error warning success info debug\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := renderer.Write([]byte("root@board:~# ")); err != nil {
		t.Fatalf("prompt Write() error = %v", err)
	}

	for _, expectedStyle := range []style{styleError, styleWarning, styleSuccess, styleInfo, styleDebug, stylePromptUser, stylePromptHost, stylePromptPath} {
		if !bytes.Contains(output.Bytes(), []byte(expectedStyle)) {
			t.Errorf("rendered output = %q, does not contain bright ANSI style %q", output.String(), expectedStyle)
		}
	}
	if bytes.Contains(output.Bytes(), []byte("\x1b[34m~")) {
		t.Errorf("rendered output = %q, uses low-contrast normal blue for the path", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte(stylePromptPath+"~"+styleReset)) {
		t.Errorf("rendered output = %q, want bright blue path styling", output.String())
	}
}

func TestRendererPassesScreenControlAndBinaryBytesThrough(t *testing.T) {
	for _, input := range [][]byte{
		[]byte("\x1b[31merror\x1b[0m\n"),
		[]byte("progress 10%\rprogress 20%\n"),
		{0xff, '\n'},
	} {
		var output bytes.Buffer
		renderer := New(&output)
		if _, err := renderer.Write(input); err != nil {
			t.Fatalf("Write(%x) error = %v", input, err)
		}
		if got := output.Bytes(); !bytes.Equal(got, input) {
			t.Errorf("Write(%x) = %x, want unchanged bytes", input, got)
		}
	}
}

func TestRendererFlushesIncompleteLine(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	if _, err := renderer.Write([]byte("not open")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := renderer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	want := string(styleError) + "not open" + string(styleReset)
	if got := output.String(); got != want {
		t.Errorf("Flush() output = %q, want %q", got, want)
	}
}
