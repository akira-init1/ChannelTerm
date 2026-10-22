package terminal

// codedTerminalError adds classification without changing readable errors or
// errors.Is behavior. The MCP adapter depends only on the ErrorCode method.
type codedTerminalError struct {
	code  string
	cause error
}

// Error preserves the existing readable diagnostic.
func (e codedTerminalError) Error() string { return e.cause.Error() }

// Unwrap preserves semantic comparison with the underlying error.
func (e codedTerminalError) Unwrap() error { return e.cause }

// ErrorCode provides the stable adapter-neutral classification.
func (e codedTerminalError) ErrorCode() string { return e.code }
