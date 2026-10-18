// Package debugger implements the local GDB/MI controller and byte-transparent
// RSP relay. It owns no terminal Channel, Session lease, or MCP endpoint.
package debugger

import (
	"fmt"
	"strings"
)

// Record is one parsed MI record. Repeated result keys are retained as lists.
// Stream text uses Go strings as byte containers; adapters must encode invalid
// UTF-8 before presenting it as JSON text.
type Record struct {
	Token   string         `json:"token,omitempty"`
	Kind    string         `json:"kind"`
	Class   string         `json:"class,omitempty"`
	Results map[string]any `json:"results,omitempty"`
	Text    string         `json:"text,omitempty"`
}

// Parse parses one bounded GDB/MI line, including nested tuples and both forms
// of MI lists. Prompt lines return a prompt record; malformed input is an error.
func Parse(line string) (Record, error) {
	if strings.TrimSpace(line) == "(gdb)" {
		return Record{Kind: "prompt"}, nil
	}
	p := miParser{s: line}
	for p.i < len(line) && line[p.i] >= '0' && line[p.i] <= '9' {
		p.i++
	}
	r := Record{Token: line[:p.i]}
	if p.i == len(line) {
		return r, fmt.Errorf("missing MI record kind")
	}
	r.Kind = line[p.i : p.i+1]
	p.i++
	switch r.Kind {
	case "~", "@", "&":
		v, err := p.value(0)
		if err != nil {
			return r, err
		}
		s, ok := v.(string)
		if !ok {
			return r, fmt.Errorf("MI stream is not a string")
		}
		r.Text = s
	case "^", "*", "+", "=":
		start := p.i
		for p.i < len(line) && line[p.i] != ',' {
			p.i++
		}
		r.Class = line[start:p.i]
		if r.Class == "" {
			return r, fmt.Errorf("empty MI class")
		}
		r.Results = map[string]any{}
		for p.i < len(line) {
			if line[p.i] != ',' {
				return r, fmt.Errorf("invalid MI result separator")
			}
			p.i++
			k, v, err := p.result(0)
			if err != nil {
				return r, err
			}
			addResult(r.Results, k, v)
		}
	default:
		return r, fmt.Errorf("invalid MI kind %q", r.Kind)
	}
	if p.i != len(line) {
		return r, fmt.Errorf("trailing MI data")
	}
	return r, nil
}

type miParser struct {
	s string
	i int
}

func addResult(m map[string]any, k string, v any) {
	if old, ok := m[k]; ok {
		if list, ok := old.([]any); ok {
			m[k] = append(list, v)
		} else {
			m[k] = []any{old, v}
		}
	} else {
		m[k] = v
	}
}
func (p *miParser) result(depth int) (string, any, error) {
	start := p.i
	for p.i < len(p.s) && ((p.s[p.i] >= 'a' && p.s[p.i] <= 'z') || (p.s[p.i] >= 'A' && p.s[p.i] <= 'Z') || (p.s[p.i] >= '0' && p.s[p.i] <= '9') || p.s[p.i] == '-' || p.s[p.i] == '_') {
		p.i++
	}
	if p.i == start || p.i >= len(p.s) || p.s[p.i] != '=' {
		return "", nil, fmt.Errorf("invalid MI result")
	}
	k := p.s[start:p.i]
	p.i++
	v, e := p.value(depth + 1)
	return k, v, e
}
func (p *miParser) value(depth int) (any, error) {
	if depth > 64 || p.i >= len(p.s) {
		return nil, fmt.Errorf("invalid MI value/depth")
	}
	switch p.s[p.i] {
	case '"':
		p.i++
		var b strings.Builder
		for p.i < len(p.s) {
			c := p.s[p.i]
			p.i++
			if c == '"' {
				return b.String(), nil
			}
			if c != '\\' {
				b.WriteByte(c)
				continue
			}
			if p.i >= len(p.s) {
				break
			}
			c = p.s[p.i]
			p.i++
			switch c {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'v':
				b.WriteByte('\v')
			case 'a':
				b.WriteByte('\a')
			case '\\', '"', '\'':
				b.WriteByte(c)
			default:
				if c < '0' || c > '7' {
					return nil, fmt.Errorf("invalid MI escape")
				}
				n := int(c - '0')
				for j := 0; j < 2 && p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '7'; j++ {
					n = n*8 + int(p.s[p.i]-'0')
					p.i++
				}
				if n > 255 {
					return nil, fmt.Errorf("MI octal overflow")
				}
				b.WriteByte(byte(n))
			}
		}
		return nil, fmt.Errorf("unterminated MI string")
	case '{':
		p.i++
		m := map[string]any{}
		if p.take('}') {
			return m, nil
		}
		for {
			k, v, e := p.result(depth + 1)
			if e != nil {
				return nil, e
			}
			addResult(m, k, v)
			if p.take('}') {
				return m, nil
			}
			if !p.take(',') {
				return nil, fmt.Errorf("invalid MI tuple")
			}
		}
	case '[':
		p.i++
		a := []any{}
		if p.take(']') {
			return a, nil
		}
		for {
			var v any
			var e error
			if p.i < len(p.s) && (p.s[p.i] == '"' || p.s[p.i] == '{' || p.s[p.i] == '[') {
				v, e = p.value(depth + 1)
			} else {
				var k string
				k, v, e = p.result(depth + 1)
				v = map[string]any{k: v}
			}
			if e != nil {
				return nil, e
			}
			a = append(a, v)
			if p.take(']') {
				return a, nil
			}
			if !p.take(',') {
				return nil, fmt.Errorf("invalid MI list")
			}
		}
	}
	return nil, fmt.Errorf("invalid MI value")
}
func (p *miParser) take(c byte) bool {
	if p.i < len(p.s) && p.s[p.i] == c {
		p.i++
		return true
	}
	return false
}

// Quote quotes one MI argument without allowing newline command injection.
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range []byte(s) {
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 32 || c >= 127:
			fmt.Fprintf(&b, "\\%03o", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
