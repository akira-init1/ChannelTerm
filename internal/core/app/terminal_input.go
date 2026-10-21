package app

// cursorPositionReply recognizes only bounded, positive DEC cursor-position
// replies. Other escapes (including arrow keys), incomplete replies and malformed
// input remain conservative pending input. This never filters terminal bytes.
func cursorPositionReply(data []byte) (complete, possible bool) {
	if len(data) > 16 || data[0] != 0x1b {
		return false, false
	}
	if len(data) == 1 {
		return false, true
	}
	if data[1] != '[' {
		return false, false
	}
	column, digits, number := false, 0, 0
	for i, b := range data[2:] {
		switch {
		case b >= '0' && b <= '9':
			digits++
			number = number*10 + int(b-'0')
			if digits > 6 {
				return false, false
			}
		case b == ';' && !column && digits > 0 && number > 0:
			column, digits, number = true, 0, 0
		case b == 'R' && column && digits > 0 && number > 0 && i+3 == len(data):
			return true, false
		default:
			return false, false
		}
	}
	return false, true
}
