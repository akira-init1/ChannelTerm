package highlight

import "regexp"

// HasInteractivePromptPrefix recognizes prompts that must be emitted raw,
// without adding a shell timestamp. It is a local presentation heuristic, not
// evidence of boot state or permission to send input. Both presentation layers
// share this predicate so neither withholds a recognized interactive line.
func HasInteractivePromptPrefix(line []byte) bool {
	if HasBootloaderPromptPrefix(line) {
		return true
	}
	for _, pattern := range interactivePromptPatterns {
		if pattern.Match(line) {
			return true
		}
	}
	return false
}

var interactivePromptPatterns = []*regexp.Regexp{
	// Minimal POSIX primary prompts and the secondary continuation prompt.
	regexp.MustCompile(`^[$#>](?:[ \t]+|$)`),
	// U-Boot variants.
	regexp.MustCompile(`^(?:=>|(?:[A-Za-z0-9_.-]+[ \t]+)*U-Boot[ \t]*>|Zynq(?:MP)?[ \t]*>)`),
	// barebox.
	regexp.MustCompile(`^barebox@[^:\r\n]+:[^ \t\r\n]+(?:[ \t]+|$)`),
	// GRUB rescue.
	regexp.MustCompile(`^grub[ \t]+rescue>`),
	// UEFI filesystem shell.
	regexp.MustCompile(`(?i)^fs[0-9]+:\\[^>\r\n]*>`),
	// Cisco ROMMON.
	regexp.MustCompile(`^(?:[A-Za-z0-9_.-]+[ \t]+)?rommon[ \t]+(?:[0-9]+[ \t]*)?>`),
	// Zephyr.
	regexp.MustCompile(`^(?:uart|rtt|usb):[^ \t\r\n]*[$#](?:[ \t]+|$)`),
	// RT-Thread.
	regexp.MustCompile(`^(?:msh|finsh)[ \t]+[^>\r\n]*>`),
	// MicroPython REPL.
	regexp.MustCompile(`^(?:>>>|\.\.\.|===)(?:[ \t]+|$)`),
	// Remote login.
	regexp.MustCompile(`(?i)^(?:(?:[A-Za-z0-9_.-]+[ \t]+)?login:|password:)(?:[ \t]+|$)`),
	// initramfs recovery.
	regexp.MustCompile(`^\(initramfs\)`),
}
