package highlight

import (
	"bytes"
	"strings"
	"testing"

	"github.com/muesli/termenv"
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

func TestRendererHighlightsNegativeWordsAndPhrases(t *testing.T) {
	input := "hwclock: can't open '/dev/misc/rtc': No such file or directory\n" +
		"FAT-fs: Volume was not properly unmounted; feature disabled; no lease\n" +
		"not\nno\n"
	var output bytes.Buffer
	if _, err := New(&output).Write([]byte(input)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	for _, phrase := range []string{"can't open", "No such", "not properly", "disabled", "not", "no"} {
		want := []byte(styleError + style(phrase) + styleReset)
		if !bytes.Contains(output.Bytes(), want) {
			t.Errorf("rendered output = %q, want negative phrase %q styled red", output.String(), phrase)
		}
	}
	if bytes.Contains(output.Bytes(), []byte(styleError+"file or directory"+styleReset)) {
		t.Errorf("rendered output = %q, want only the negative marker rather than its trailing description styled red", output.String())
	}
}

func TestRendererHighlightsQuotedValuesWithRequestedYellow(t *testing.T) {
	input := "paths: '/dev/misc/rtc' '/dev/1' \"/dev/ttyS0\" “device” ‘serial’\n"
	var output bytes.Buffer
	if _, err := New(&output).Write([]byte(input)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	quotedStyle := defaultTheme.style(tokenString)
	if quotedStyle != "\x1b[38;2;203;169;75m" {
		t.Fatalf("quoted style = %q, want #CBA94B TrueColor style", quotedStyle)
	}
	for _, value := range []string{"'/dev/misc/rtc'", "'/dev/1'", "\"/dev/ttyS0\"", "“device”", "‘serial’"} {
		if !bytes.Contains(output.Bytes(), []byte(quotedStyle+style(value)+styleReset)) {
			t.Errorf("rendered output = %q, want quoted value %q styled yellow", output.String(), value)
		}
	}
}

func TestRendererHighlightsCompleteDates(t *testing.T) {
	dates := []string{
		"Fri Mar 9 12:34:56 UTC 2018",
		"Mar 9 12:34:56 UTC 2018",
		"Mon Apr 11 17:52:14 UTC 2022",
		"Apr 04 2022 - 07:53:54 +0000",
		"2026-09-17 18:30:45+08:00",
	}
	var output bytes.Buffer
	renderer := New(&output)
	for _, date := range dates {
		if _, err := renderer.Write([]byte("time: " + date + "\n")); err != nil {
			t.Fatalf("Write(%q) error = %v", date, err)
		}
	}
	dateStyle := defaultTheme.style(tokenTimestamp)
	for _, date := range dates {
		if !bytes.Contains(output.Bytes(), []byte(dateStyle+style(date)+styleReset)) {
			t.Errorf("rendered output = %q, want complete date %q styled as one token", output.String(), date)
		}
	}
}

func TestRendererUsesOnePurpleForNumericBasesAndVersions(t *testing.T) {
	values := []string{
		"0x1A2f",
		"0b101010",
		"0o755",
		"8'hFF",
		"DEADBEEF",
		"0755",
		"1024",
		"2.99",
		"6.02e23",
	}
	var output bytes.Buffer
	renderer := New(&output)
	for _, value := range values {
		if _, err := renderer.Write([]byte("value: " + value + "\n")); err != nil {
			t.Fatalf("Write(%q) error = %v", value, err)
		}
	}
	numberStyle := defaultTheme.style(tokenNumber)
	if defaultTheme.style(tokenAddress) != numberStyle || defaultTheme.style(tokenVersion) != numberStyle {
		t.Fatalf("numeric styles differ: number=%q address=%q version=%q", numberStyle, defaultTheme.style(tokenAddress), defaultTheme.style(tokenVersion))
	}
	for _, value := range values {
		if !bytes.Contains(output.Bytes(), []byte(numberStyle+style(value)+styleReset)) {
			t.Errorf("rendered output = %q, want numeric value %q styled with the shared purple", output.String(), value)
		}
	}
	var unitOutput bytes.Buffer
	if _, err := New(&unitOutput).Write([]byte("memory: 1024K\n")); err != nil {
		t.Fatalf("Write(number with unit) error = %v", err)
	}
	if !bytes.Contains(unitOutput.Bytes(), []byte(numberStyle+"1024"+styleReset+"K")) {
		t.Errorf("rendered output = %q, want number before an attached unit styled purple", unitOutput.String())
	}
}

func TestRendererHighlightsEmbeddedInterfacesAndInstanceNumbers(t *testing.T) {
	input := "7 fixed-partitions partitions found on MTD device spi0.0\n" +
		"mmcblk0boot1: mmc0:0001 4.00 MiB\n"
	var output bytes.Buffer
	if _, err := New(&output).Write([]byte(input)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	interfaceStyle := defaultTheme.style(tokenInterface)
	numberStyle := defaultTheme.style(tokenNumber)
	for _, token := range []string{"MTD", "spi", "mmcblk", "mmc"} {
		if !bytes.Contains(output.Bytes(), []byte(interfaceStyle+style(token)+styleReset)) {
			t.Errorf("rendered output = %q, want interface token %q highlighted", output.String(), token)
		}
	}
	for _, number := range []string{"7", "0.0", "0", "1", "0001", "4.00"} {
		if !bytes.Contains(output.Bytes(), []byte(numberStyle+style(number)+styleReset)) {
			t.Errorf("rendered output = %q, want embedded number %q highlighted purple", output.String(), number)
		}
	}
}

func TestRendererMatchesEmbeddedInterfacesCaseInsensitively(t *testing.T) {
	interfaces := []string{
		"SPI", "spi", "SpI",
		"CAN", "can", "Can",
		"I2C", "i2c", "I2c",
		"UART", "uart",
		"USB", "usb",
		"PCIe", "pcie",
		"GPIO", "gpio",
	}
	var output bytes.Buffer
	renderer := New(&output)
	for _, name := range interfaces {
		if _, err := renderer.Write([]byte("interface " + name + " ready\n")); err != nil {
			t.Fatalf("Write(%q) error = %v", name, err)
		}
	}
	interfaceStyle := defaultTheme.style(tokenInterface)
	for _, name := range interfaces {
		if !bytes.Contains(output.Bytes(), []byte(interfaceStyle+style(name)+styleReset)) {
			t.Errorf("rendered output = %q, want case-insensitive interface %q highlighted", output.String(), name)
		}
	}
}

func TestRendererHighlightsDelimitersWithRequestedGreen(t *testing.T) {
	input := "(value) [value] {value} '[quoted]'\n"
	var output bytes.Buffer
	if _, err := New(&output).Write([]byte(input)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	delimiterStyle := defaultTheme.style(tokenDelimiter)
	if delimiterStyle != "\x1b[38;2;109;177;41m" {
		t.Fatalf("delimiter style = %q, want #6DB129 TrueColor style", delimiterStyle)
	}
	if got := bytes.Count(output.Bytes(), []byte(delimiterStyle)); got != 6 {
		t.Errorf("rendered output = %q, delimiter style count = %d, want 6 outside quotes", output.String(), got)
	}
	quoted := []byte(defaultTheme.style(tokenString) + "'[quoted]'" + styleReset)
	if !bytes.Contains(output.Bytes(), quoted) {
		t.Errorf("rendered output = %q, want quoted delimiters to retain quoted-value color", output.String())
	}
}

func TestRendererKeepsCompleteDateDistinctFromNumbers(t *testing.T) {
	const date = "Fri Mar 9 12:34:56 UTC 2018"
	var output bytes.Buffer
	if _, err := New(&output).Write([]byte("date: " + date + " count: 1024\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	dateStyle := defaultTheme.style(tokenTimestamp)
	numberStyle := defaultTheme.style(tokenNumber)
	if dateStyle == numberStyle {
		t.Fatal("date and number styles must remain distinct")
	}
	if !bytes.Contains(output.Bytes(), []byte(dateStyle+date+styleReset)) {
		t.Errorf("rendered output = %q, want complete date in date color", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte(numberStyle+"1024"+styleReset)) {
		t.Errorf("rendered output = %q, want ordinary number in shared purple", output.String())
	}
}

func TestRendererUsesErrorRedForWarnings(t *testing.T) {
	var output bytes.Buffer
	if _, err := New(&output).Write([]byte("Warning: retry\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if styleWarning != styleError {
		t.Fatalf("warning style = %q, error style = %q; want the same red", styleWarning, styleError)
	}
	if !bytes.Contains(output.Bytes(), []byte(styleError+"Warning"+styleReset)) {
		t.Errorf("rendered output = %q, want Warning styled red", output.String())
	}
}

func TestRendererUsesDarkGoldForBootStagesAndUnits(t *testing.T) {
	const darkGold = style("\x1b[38;2;203;169;75m")
	if got := defaultTheme.style(tokenHeading); got != darkGold {
		t.Fatalf("heading style = %q, want dark gold #CBA94B", got)
	}
	if got := defaultTheme.style(tokenUnit); got != darkGold {
		t.Fatalf("unit style = %q, want dark gold #CBA94B", got)
	}

	var output bytes.Buffer
	if _, err := New(&output).Write([]byte("## Booting kernel from Legacy Image: 4.6 MiB\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte(defaultTheme.style(tokenDelimiter)+"##"+styleReset)) {
		t.Errorf("rendered output = %q, want ## styled with structural green", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte(darkGold+"Booting kernel"+styleReset)) {
		t.Errorf("rendered output = %q, want boot stage styled dark gold", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte(darkGold+"MiB"+styleReset)) {
		t.Errorf("rendered output = %q, want unit styled dark gold", output.String())
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

func TestRendererDoesNotHighlightUserCommandInSameReadAsPrompt(t *testing.T) {
	input := []byte("root@pdprj:~# echo no not disabled can't open '/dev/misc/rtc'\r\n")
	var output bytes.Buffer
	if _, err := New(&output).Write(input); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte(stylePromptUser+"root"+styleReset)) {
		t.Errorf("rendered output = %q, want styled prompt", output.String())
	}
	if bytes.Contains(output.Bytes(), []byte(styleError)) {
		t.Errorf("rendered output = %q, user command must not receive semantic error styling", output.String())
	}
	if bytes.Contains(output.Bytes(), []byte(defaultTheme.style(tokenString))) {
		t.Errorf("rendered output = %q, user command must not receive quoted-value styling", output.String())
	}
}

func TestRendererStylesRapidEmptyPromptsAcrossReadChunks(t *testing.T) {
	input := []byte("root@pdprj:~# \r\nroot@pdprj:~# \r\nroot@pdprj:~# ")
	var baseline bytes.Buffer
	if _, err := New(&baseline).Write(input); err != nil {
		t.Fatalf("baseline Write() error = %v", err)
	}
	styledUser := []byte(stylePromptUser + "root" + styleReset)
	if got := bytes.Count(baseline.Bytes(), styledUser); got != 3 {
		t.Fatalf("baseline styled prompt count = %d, want 3; output = %q", got, baseline.String())
	}

	for split := 0; split <= len(input); split++ {
		var output bytes.Buffer
		renderer := New(&output)
		if _, err := renderer.Write(input[:split]); err != nil {
			t.Fatalf("split %d first Write() error = %v", split, err)
		}
		if _, err := renderer.Write(input[split:]); err != nil {
			t.Fatalf("split %d second Write() error = %v", split, err)
		}
		if got := output.Bytes(); !bytes.Equal(got, baseline.Bytes()) {
			t.Errorf("split %d output = %q, want chunk-independent %q", split, got, baseline.Bytes())
		}
	}
}

func TestRendererStylesTimestampedPromptBeforeNewline(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	input := "[14:05:01] root@board:~# "
	if _, err := renderer.Write([]byte(input)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !bytes.HasPrefix(output.Bytes(), []byte("[14:05:01] ")) {
		t.Errorf("output = %q, want unchanged timestamp prefix", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte(stylePromptUser+"root"+styleReset)) {
		t.Errorf("output = %q, want styled prompt user", output.String())
	}
}

func TestRendererFlushResetsPromptEchoStateBeforeLocalStatus(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	prompt := []byte("root@board:~# ")
	if _, err := renderer.Write(prompt); err != nil {
		t.Fatalf("first prompt Write() error = %v", err)
	}
	if err := renderer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if _, err := renderer.Write(prompt); err != nil {
		t.Fatalf("second prompt Write() error = %v", err)
	}
	if got := bytes.Count(output.Bytes(), []byte(stylePromptUser+"root"+styleReset)); got != 2 {
		t.Errorf("styled prompt count = %d, want 2 after local presentation boundary; output = %q", got, output.String())
	}
}

func TestRendererUsesWinTermTrueColorStyles(t *testing.T) {
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
			t.Errorf("rendered output = %q, does not contain WinTerm ANSI style %q", output.String(), expectedStyle)
		}
	}
	if bytes.Contains(output.Bytes(), []byte("\x1b[34m~")) {
		t.Errorf("rendered output = %q, uses low-contrast normal blue for the path", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte(stylePromptPath+"~"+styleReset)) {
		t.Errorf("rendered output = %q, want bright blue path styling", output.String())
	}
	if bytes.Contains(output.Bytes(), []byte("\x1b[1;")) {
		t.Errorf("rendered output = %q, want color without generated bold attributes", output.String())
	}
	if stylePromptUser != "\x1b[31m" || stylePromptHost != "\x1b[36m" || stylePromptPath != "\x1b[94m" {
		t.Errorf("prompt styles = %q, %q, %q; want original red/cyan/bright-blue palette without bold", stylePromptUser, stylePromptHost, stylePromptPath)
	}
	if styleDebug != "\x1b[95m" {
		t.Errorf("debug style = %q, want original bright-magenta palette without bold", styleDebug)
	}
}

func TestRendererUsesMutedFieldColorAroundEmbeddedTokens(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	if _, err := renderer.Write([]byte("mmcblk0boot1: mmc0:0001 4.00 MiB\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	fieldStyle := defaultTheme.style(tokenField)
	for _, fragment := range []string{"boot", ":"} {
		if !bytes.Contains(output.Bytes(), []byte(fieldStyle+style(fragment)+styleReset)) {
			t.Errorf("rendered output = %q, want field fragment %q in muted field color", output.String(), fragment)
		}
	}
	if fieldStyle != "\x1b[38;2;86;156;214m" {
		t.Errorf("field style = %q, want muted blue TrueColor style", fieldStyle)
	}
}

func TestRendererHighlightsNoAsErrorWithoutBold(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	if _, err := renderer.Write([]byte("udhcp: no lease, forking to background\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte(styleError+"no"+styleReset)) {
		t.Errorf("rendered output = %q, want no styled as an error", output.String())
	}
	if bytes.Contains(output.Bytes(), []byte("\x1b[1;")) {
		t.Errorf("rendered output = %q, want no generated bold attributes", output.String())
	}
}

func TestRendererASCIIProfileLeavesPromptUnstyled(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, WithColorProfile(termenv.Ascii))
	const prompt = "root@pdprj:~# "
	if _, err := renderer.Write([]byte(prompt)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := output.String(); got != prompt {
		t.Errorf("rendered prompt = %q, want unstyled %q", got, prompt)
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

func TestRendererFlushesIncompleteLineUnchanged(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	if _, err := renderer.Write([]byte("not open")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := renderer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	want := "not open"
	if got := output.String(); got != want {
		t.Errorf("Flush() output = %q, want %q", got, want)
	}
}

func TestRendererHighlightsBootTokens(t *testing.T) {
	input := "U-Boot 2022.01 (Apr 04 2022 - 07:53:54 +0000)\n" +
		"DRAM: ECC disabled 1 GiB\n" +
		"*** Warning - bad CRC, using default environment\n" +
		"Load Address: 00200000\n" +
		"Verifying Checksum ... OK\n"
	var output bytes.Buffer
	renderer := New(&output)
	if _, err := renderer.Write([]byte(input)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	wantTokens := [][]byte{
		[]byte(defaultTheme.style(tokenHeading) + "U-Boot" + styleReset),
		[]byte(defaultTheme.style(tokenVersion) + "2022.01" + styleReset),
		[]byte(defaultTheme.style(tokenField) + "DRAM:" + styleReset),
		[]byte(defaultTheme.style(tokenError) + "disabled" + styleReset),
		[]byte(defaultTheme.style(tokenWarning) + "Warning" + styleReset),
		[]byte(defaultTheme.style(tokenField) + "Load Address:" + styleReset),
		[]byte(defaultTheme.style(tokenAddress) + "00200000" + styleReset),
		[]byte(defaultTheme.style(tokenSuccess) + "OK" + styleReset),
	}
	for _, token := range wantTokens {
		if !bytes.Contains(output.Bytes(), token) {
			t.Errorf("rendered output = %q, does not contain styled token %q", output.String(), token)
		}
	}
}

func TestRendererBootOutputDoesNotDependOnReadChunks(t *testing.T) {
	input := []byte("U-Boot 2022.01\r\nDRAM: ECC disabled 1 GiB\r\nLoad Address: 00200000\r\nVerifying Checksum ... OK\r\n")
	var baseline bytes.Buffer
	if _, err := New(&baseline).Write(input); err != nil {
		t.Fatalf("baseline Write() error = %v", err)
	}

	for split := 0; split <= len(input); split++ {
		var output bytes.Buffer
		renderer := New(&output)
		if _, err := renderer.Write(input[:split]); err != nil {
			t.Fatalf("split %d first Write() error = %v", split, err)
		}
		if _, err := renderer.Write(input[split:]); err != nil {
			t.Fatalf("split %d second Write() error = %v", split, err)
		}
		if got := output.Bytes(); !bytes.Equal(got, baseline.Bytes()) {
			t.Errorf("split %d output = %q, want %q", split, got, baseline.Bytes())
		}
	}
}

func TestRendererResumesHighlightingAfterSplitRemoteSGRReset(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	if _, err := renderer.Write([]byte("partial ")); err != nil {
		t.Fatalf("plain Write() error = %v", err)
	}
	if _, err := renderer.Write([]byte("\x1b")); err != nil {
		t.Fatalf("escape Write() error = %v", err)
	}
	if _, err := renderer.Write([]byte("[31mremote\x1b[0m\nerror 00200000\n")); err != nil {
		t.Fatalf("raw Write() error = %v", err)
	}
	wantRawPrefix := "partial \x1b[31mremote\x1b[0m\n"
	if got := output.String(); !strings.HasPrefix(got, wantRawPrefix) {
		t.Errorf("output = %q, want byte-transparent prefix %q", got, wantRawPrefix)
	}
	if !bytes.Contains(output.Bytes(), []byte(styleError+"error"+styleReset)) {
		t.Errorf("output = %q, want semantic highlighting after remote SGR reset", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte(defaultTheme.style(tokenAddress)+"00200000"+styleReset)) {
		t.Errorf("output = %q, want address highlighting after remote SGR reset", output.String())
	}
}

func TestRendererKeepsPersistentSGRRawUntilReset(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	raw := "\x1b[31mremote\nerror 00200000\n\x1b[0m\n"
	if _, err := renderer.Write([]byte(raw)); err != nil {
		t.Fatalf("raw Write() error = %v", err)
	}
	if got := output.String(); got != raw {
		t.Fatalf("output = %q, want persistent SGR bytes unchanged %q", got, raw)
	}
	if _, err := renderer.Write([]byte("error 00200000\n")); err != nil {
		t.Fatalf("plain Write() error = %v", err)
	}
	if !bytes.Contains(output.Bytes()[len(raw):], []byte(styleError+"error"+styleReset)) {
		t.Errorf("output suffix = %q, want highlighting after SGR reset", output.Bytes()[len(raw):])
	}
}

func TestRendererStylesFirstPromptAfterPresentationNeutralEscape(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
	}{
		{name: "OSC title terminated by BEL", prefix: "\x1b]0;root@pdprj:~\x07"},
		{name: "OSC title terminated by ST", prefix: "\x1b]0;root@pdprj:~\x1b\\"},
		{name: "SGR reset", prefix: "\x1b[0m"},
		{name: "cursor visible", prefix: "\x1b[?25h"},
		{name: "bracketed paste", prefix: "\x1b[?2004h"},
		{name: "combined prompt modes", prefix: "\x1b[?25;2004h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.prefix + "root@pdprj:~# ")
			var baseline bytes.Buffer
			if _, err := New(&baseline).Write(input); err != nil {
				t.Fatalf("baseline Write() error = %v", err)
			}
			if !bytes.HasPrefix(baseline.Bytes(), []byte(tt.prefix)) {
				t.Errorf("baseline output = %q, want raw escape prefix %q", baseline.String(), tt.prefix)
			}
			if !bytes.Contains(baseline.Bytes(), []byte(stylePromptUser+"root"+styleReset)) {
				t.Fatalf("baseline output = %q, want styled first prompt", baseline.String())
			}

			for split := 0; split <= len(input); split++ {
				var output bytes.Buffer
				renderer := New(&output)
				if _, err := renderer.Write(input[:split]); err != nil {
					t.Fatalf("split %d first Write() error = %v", split, err)
				}
				if _, err := renderer.Write(input[split:]); err != nil {
					t.Fatalf("split %d second Write() error = %v", split, err)
				}
				if got := output.Bytes(); !bytes.Equal(got, baseline.Bytes()) {
					t.Errorf("split %d output = %q, want chunk-independent %q", split, got, baseline.Bytes())
				}
			}
		})
	}
}

func TestRendererDoesNotResumePromptHighlightingAfterScreenControl(t *testing.T) {
	input := []byte("\x1b[2Jroot@pdprj:~# ")
	var output bytes.Buffer
	if _, err := New(&output).Write(input); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := output.Bytes(); !bytes.Equal(got, input) {
		t.Errorf("output = %q, want screen-control stream unchanged %q", got, input)
	}
}

func TestRendererKeepsAlternateScreenRawUntilExit(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output)
	raw := "\x1b[?1049h\x1b[2Jerror 00200000\n\x1b[H\x1b[?1049l\r\n"
	for _, chunk := range [][]byte{
		[]byte("\x1b[?10"),
		[]byte("49h\x1b[2Jerror 00200000\n\x1b[H\x1b[?1049"),
		[]byte("l\r\n"),
	} {
		if _, err := renderer.Write(chunk); err != nil {
			t.Fatalf("raw Write(%q) error = %v", chunk, err)
		}
	}
	if got := output.String(); got != raw {
		t.Fatalf("output = %q, want alternate-screen bytes unchanged %q", got, raw)
	}
	if _, err := renderer.Write([]byte("warning after exit\n")); err != nil {
		t.Fatalf("plain Write() error = %v", err)
	}
	if !bytes.Contains(output.Bytes()[len(raw):], []byte(styleWarning+"warning"+styleReset)) {
		t.Errorf("output suffix = %q, want highlighting after alternate-screen exit", output.Bytes()[len(raw):])
	}
}

func TestRendererColorProfilesDegradeOrDisableStyles(t *testing.T) {
	tests := []struct {
		name            string
		profile         termenv.Profile
		wantANSI        bool
		forbidTrueColor bool
	}{
		{name: "truecolor", profile: termenv.TrueColor, wantANSI: true},
		{name: "ansi256", profile: termenv.ANSI256, wantANSI: true, forbidTrueColor: true},
		{name: "ansi16", profile: termenv.ANSI, wantANSI: true, forbidTrueColor: true},
		{name: "ascii", profile: termenv.Ascii},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			renderer := New(&output, WithColorProfile(tt.profile))
			if _, err := renderer.Write([]byte("Warning at 0x00200000: OK\n")); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			hasANSI := bytes.Contains(output.Bytes(), []byte("\x1b["))
			if hasANSI != tt.wantANSI {
				t.Errorf("output = %q, ANSI presence = %t, want %t", output.String(), hasANSI, tt.wantANSI)
			}
			if tt.forbidTrueColor && bytes.Contains(output.Bytes(), []byte("38;2;")) {
				t.Errorf("output = %q, want downgraded colors without TrueColor sequences", output.String())
			}
			if tt.profile == termenv.Ascii && output.String() != "Warning at 0x00200000: OK\n" {
				t.Errorf("ASCII output = %q, want unchanged text", output.String())
			}
		})
	}
}
