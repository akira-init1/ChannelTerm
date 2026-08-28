package serial

import (
	"fmt"
	"strconv"
	"strings"
)

// normalizeUSBID converts an operating-system USB identifier into the stable
// four-digit lowercase hexadecimal form exposed by DeviceMetadata. Invalid
// identifiers remain empty instead of being guessed or passed through with a
// platform-specific prefix.
func normalizeUSBID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "0x")
	if value == "" {
		return ""
	}
	number, err := strconv.ParseUint(value, 16, 16)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%04x", number)
}
