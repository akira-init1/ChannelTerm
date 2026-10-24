package command

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"golang.org/x/text/encoding"
	"golang.org/x/text/transform"
)

// terminalPreferences belong to one client invocation, never Transport or Host metadata.
type terminalPreferences struct {
	encoding, view, description string
}

func defineTerminalFlags(flags *flag.FlagSet) *terminalPreferences {
	p := &terminalPreferences{}
	flags.StringVar(&p.encoding, "encoding", "utf-8", "client-local terminal encoding: "+terminalEncodingNames())
	flags.StringVar(&p.view, "view", "text", "terminal display: text or read-only hex (client-local)")
	flags.StringVar(&p.description, "description", "", "local connection description; not sent to the device or saved in Host metadata")
	return p
}

func (p *terminalPreferences) codec() (*terminalCodec, error) {
	p.encoding = strings.ToLower(strings.TrimSpace(p.encoding))
	if p.encoding == "utf8" {
		p.encoding = "utf-8"
	}
	if p.view != "text" && p.view != "hex" {
		return nil, errors.New("--view must be text or hex")
	}
	if !utf8.ValidString(p.description) || len(p.description) > 512 || strings.IndexFunc(p.description, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return nil, errors.New("--description must be a single line of at most 512 UTF-8 bytes without control characters")
	}
	name, codec, ok := resolveTerminalEncoding(p.encoding)
	if !ok {
		return nil, fmt.Errorf("unsupported --encoding %q; supported encodings: %s", p.encoding, terminalEncodingNames())
	}
	p.encoding = name
	c := &terminalCodec{hex: p.view == "hex", ascii: name == "ascii", encoding: codec}
	if c.encoding != nil {
		c.decoder = c.encoding.NewDecoder()
	}
	return c, nil
}

func (p *terminalPreferences) args() []string {
	return []string{"--encoding", p.encoding, "--view", p.view, "--description", p.description}
}

// terminalCodec has independent input/output state. Each direction has one owner;
// output calls use the existing presentation mutex. Raw transfer paths never use it.
type terminalCodec struct {
	encoding              encoding.Encoding
	decoder               *encoding.Decoder
	inputTail, outputTail []byte
	hex                   bool
	ascii                 bool
	offset                uint64
}

func (c *terminalCodec) output(data []byte, eof bool) ([]byte, error) {
	if c.hex {
		var out strings.Builder
		for len(data) > 0 {
			n := min(len(data), 16)
			row := data[:n]
			fmt.Fprintf(&out, "%08x  ", c.offset)
			for i := 0; i < 16; i++ {
				if i < n {
					fmt.Fprintf(&out, "%02x ", row[i])
				} else {
					out.WriteString("   ")
				}
			}
			out.WriteString(" |")
			for _, b := range row {
				if b >= 32 && b < 127 {
					out.WriteByte(b)
				} else {
					out.WriteByte('.')
				}
			}
			out.WriteString("|\r\n")
			c.offset += uint64(n)
			data = data[n:]
		}
		return []byte(out.String()), nil
	}
	if c.ascii {
		var out strings.Builder
		for _, b := range data {
			if b < 128 {
				out.WriteByte(b)
			} else {
				out.WriteRune(utf8.RuneError)
			}
		}
		return []byte(out.String()), nil
	}
	if c.decoder == nil {
		return data, nil
	} // Preserve the established UTF-8/raw default.
	source := append(c.outputTail, data...)
	dest := make([]byte, len(source)*3+4)
	n, used, err := c.decoder.Transform(dest, source, eof)
	c.outputTail = append(c.outputTail[:0], source[used:]...)
	if errors.Is(err, transform.ErrShortSrc) && !eof {
		err = nil
	}
	if len(c.outputTail) > 4 {
		return nil, errors.New("terminal decoder retained an oversized character")
	}
	return dest[:n], err
}

func (c *terminalCodec) resetOutput() {
	c.outputTail = nil
	c.offset = 0
	if c.decoder != nil {
		c.decoder.Reset()
	}
}

// Input is UTF-8 from the local console. A rejected/unrepresentable batch is
// discarded atomically, never replaced with '?' or replayed after a failed write.
func (c *terminalCodec) input(data []byte) ([]byte, error) {
	if c.hex {
		return nil, errors.New("hex view is read-only; use --view text for terminal input")
	}
	if c.ascii {
		for _, b := range data {
			if b >= 128 {
				return nil, errors.New("input cannot be represented in ASCII")
			}
		}
		return data, nil
	}
	if c.encoding == nil {
		return data, nil
	}
	source := append(c.inputTail, data...)
	c.inputTail = nil
	end := 0
	for end < len(source) {
		if !utf8.FullRune(source[end:]) {
			break
		}
		r, size := utf8.DecodeRune(source[end:])
		if r == utf8.RuneError && size == 1 {
			return nil, errors.New("terminal input must be valid UTF-8")
		}
		end += size
	}
	encoded, err := c.encoding.NewEncoder().Bytes(source[:end])
	if err != nil {
		return nil, fmt.Errorf("input cannot be represented in selected encoding: %w", err)
	}
	c.inputTail = append([]byte(nil), source[end:]...)
	return encoded, nil
}

type encodedTerminalWriter struct {
	target interface {
		Write(session.WriteRequest) (int, error)
	}
	codec *terminalCodec
}

func (w encodedTerminalWriter) Write(request session.WriteRequest) (int, error) {
	data, err := w.codec.input(request.Data)
	if err != nil {
		return 0, err
	}
	if len(data) > 0 {
		err = writeSession(w.target, request.Actor, data)
		if err != nil {
			w.codec.inputTail = nil
			return 0, err
		}
	}
	return len(request.Data), nil
}
