//go:build !windows

package command

import "os"

// enableANSIOutput reports that a terminal selected by auto mode accepts ANSI
// SGR output on platforms whose native terminals use ANSI escape sequences.
func enableANSIOutput(_ *os.File) bool { return true }
