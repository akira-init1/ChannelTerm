package serial

import "testing"

func TestNormalizeUSBID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "0403", want: "0403"},
		{input: "0x1A86", want: "1a86"},
		{input: "7523\n", want: "7523"},
		{input: "", want: ""},
		{input: "not-a-usb-id", want: ""},
		{input: "10000", want: ""},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			if got := normalizeUSBID(test.input); got != test.want {
				t.Errorf("normalizeUSBID(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}
