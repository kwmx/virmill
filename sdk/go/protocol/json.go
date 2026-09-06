// Package protocol validates bounded, unambiguous JSON before decoding typed messages.
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const MaxFrame = 8 << 20
const MaxDepth = 64

// Validate rejects duplicate keys (including escaped equivalents), invalid UTF-8,
// unpaired UTF-16 surrogates, excessive nesting and trailing JSON values.
func Validate(data []byte) error {
	if len(data) == 0 || len(data) > MaxFrame {
		return errors.New("JSON frame size outside bounds")
	}
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8")
	}
	if err := surrogates(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > MaxDepth {
			return errors.New("JSON nesting limit exceeded")
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		switch t {
		case json.Delim('{'):
			keys := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				s, ok := k.(string)
				if !ok {
					return errors.New("object key is not string")
				}
				if keys[s] {
					return fmt.Errorf("duplicate JSON key %q", s)
				}
				keys[s] = true
				if e = value(depth + 1); e != nil {
					return e
				}
			}
			t, e = d.Token()
			if e != nil {
				return e
			}
			if t != json.Delim('}') {
				return errors.New("unclosed object")
			}
		case json.Delim('['):
			for d.More() {
				if e = value(depth + 1); e != nil {
					return e
				}
			}
			t, e = d.Token()
			if e != nil {
				return e
			}
			if t != json.Delim(']') {
				return errors.New("unclosed array")
			}
		default:
			if _, ok := t.(json.Delim); ok {
				return errors.New("unexpected delimiter")
			}
		}
		return nil
	}
	if e := value(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing JSON value")
	}
	return nil
}
func surrogates(b []byte) error {
	in := false
	hex := func(x []byte) (uint16, bool) {
		var n uint16
		for _, c := range x {
			n *= 16
			switch {
			case c >= '0' && c <= '9':
				n += uint16(c - '0')
			case c >= 'a' && c <= 'f':
				n += uint16(c - 'a' + 10)
			case c >= 'A' && c <= 'F':
				n += uint16(c - 'A' + 10)
			default:
				return 0, false
			}
		}
		return n, true
	}
	for i := 0; i < len(b); i++ {
		if b[i] == '"' {
			in = !in
			continue
		}
		if !in || b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return errors.New("invalid string escape")
		}
		if b[i] != 'u' {
			continue
		}
		if i+4 >= len(b) {
			return errors.New("short unicode escape")
		}
		n, ok := hex(b[i+1 : i+5])
		if !ok {
			return errors.New("invalid unicode escape")
		}
		i += 4
		if n >= 0xDC00 && n <= 0xDFFF {
			return errors.New("unpaired low surrogate")
		}
		if n >= 0xD800 && n <= 0xDBFF {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return errors.New("unpaired high surrogate")
			}
			m, ok := hex(b[i+3 : i+7])
			if !ok || m < 0xDC00 || m > 0xDFFF {
				return errors.New("unpaired high surrogate")
			}
			i += 6
		}
	}
	return nil
}
func Decode(data []byte, dst any) error {
	if err := Validate(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(dst)
}

// ReadFrame never allocates beyond the protocol bound, including a missing newline.
func ReadFrame(r io.Reader) ([]byte, error) {
	var out bytes.Buffer
	b := make([]byte, 1)
	for out.Len() <= MaxFrame {
		n, e := r.Read(b)
		if n > 0 {
			if b[0] == '\n' {
				return out.Bytes(), nil
			}
			out.WriteByte(b[0])
		}
		if e != nil {
			if e == io.EOF && out.Len() > 0 {
				return nil, errors.New("unterminated frame")
			}
			return nil, e
		}
	}
	return nil, errors.New("oversized frame")
}
