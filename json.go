package main

// A JSON reader and writer sized to what pin actually handles: the registry in
// pins.json (objects of strings, one string array, one optional string) and the
// `version` / `bin` members of an arbitrary package.json.
//
// encoding/json is not used because it is 900KB of the 3MB binary — a third of
// the file, for two field types and one small file. This is a strict reader: it
// rejects anything malformed rather than guessing, and it skips what it does not
// need without materialising it.

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type jreader struct {
	b []byte
	i int
}

func (r *jreader) errf(msg string) error {
	return fmt.Errorf("json: %s at byte %d", msg, r.i)
}

func (r *jreader) skipWS() {
	for r.i < len(r.b) {
		switch r.b[r.i] {
		case ' ', '\t', '\n', '\r':
			r.i++
		default:
			return
		}
	}
}

func (r *jreader) peek(c byte) bool { return r.i < len(r.b) && r.b[r.i] == c }

func (r *jreader) expect(c byte) error {
	if !r.peek(c) {
		return r.errf(fmt.Sprintf("expected %q", c))
	}
	r.i++
	return nil
}

// str reads a JSON string. The common case (no escapes) is a slice of the input;
// escapes go through a builder only when the string actually has one.
func (r *jreader) str() (string, error) {
	if err := r.expect('"'); err != nil {
		return "", err
	}
	start := r.i
	for r.i < len(r.b) {
		c := r.b[r.i]
		switch {
		case c == '"':
			s := string(r.b[start:r.i])
			r.i++
			return s, nil
		case c == '\\':
			return r.strEscaped(start)
		case c < 0x20:
			return "", r.errf("control character in string")
		}
		r.i++
	}
	return "", r.errf("unterminated string")
}

func (r *jreader) strEscaped(start int) (string, error) {
	var sb strings.Builder
	sb.Write(r.b[start:r.i])
	for r.i < len(r.b) {
		switch c := r.b[r.i]; {
		case c == '"':
			r.i++
			return sb.String(), nil
		case c == '\\':
			r.i++
			if r.i >= len(r.b) {
				return "", r.errf("truncated escape")
			}
			switch r.b[r.i] {
			case '"', '\\', '/':
				sb.WriteByte(r.b[r.i])
				r.i++
			case 'b':
				sb.WriteByte('\b')
				r.i++
			case 'f':
				sb.WriteByte('\f')
				r.i++
			case 'n':
				sb.WriteByte('\n')
				r.i++
			case 'r':
				sb.WriteByte('\r')
				r.i++
			case 't':
				sb.WriteByte('\t')
				r.i++
			case 'u':
				hi, err := r.hex4()
				if err != nil {
					return "", err
				}
				// A high surrogate is followed by its low half as another \uXXXX.
				if utf16.IsSurrogate(rune(hi)) && r.i+1 < len(r.b) &&
					r.b[r.i] == '\\' && r.b[r.i+1] == 'u' {
					r.i++ // onto the 'u' of the second escape
					lo, err := r.hex4()
					if err != nil {
						return "", err
					}
					if dec := utf16.DecodeRune(rune(hi), rune(lo)); dec != utf8.RuneError {
						sb.WriteRune(dec)
					} else {
						// Not a pair after all (low then high, say): two replacement
						// characters, which is what encoding/json produces too.
						sb.WriteRune(utf8.RuneError)
						sb.WriteRune(rune(lo))
					}
					continue
				}
				sb.WriteRune(rune(hi))
			default:
				return "", r.errf("unknown escape")
			}
		case c < 0x20:
			return "", r.errf("control character in string")
		default:
			sb.WriteByte(c)
			r.i++
		}
	}
	return "", r.errf("unterminated string")
}

func (r *jreader) hex4() (uint32, error) {
	if r.i+4 >= len(r.b) {
		return 0, r.errf("truncated \\u escape")
	}
	var v uint32
	for k := 1; k <= 4; k++ {
		c := r.b[r.i+k]
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= uint32(c - '0')
		case c >= 'a' && c <= 'f':
			v |= uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v |= uint32(c-'A') + 10
		default:
			return 0, r.errf("bad hex in \\u escape")
		}
	}
	r.i += 5 // the 'u' and its four digits
	return v, nil
}

func (r *jreader) word(w string) error {
	if r.i+len(w) > len(r.b) || string(r.b[r.i:r.i+len(w)]) != w {
		return r.errf("expected " + w)
	}
	r.i += len(w)
	return nil
}

// number validates the JSON number grammar without converting: pin reads no
// numbers, it only has to get past them.
func (r *jreader) number() error {
	if r.peek('-') {
		r.i++
	}
	digits := func() int {
		n := 0
		for r.i < len(r.b) && r.b[r.i] >= '0' && r.b[r.i] <= '9' {
			r.i++
			n++
		}
		return n
	}
	// One leading zero, never two: 0 is a number, 00 is a leaky parser.
	if r.peek('0') {
		r.i++
		if r.i < len(r.b) && r.b[r.i] >= '0' && r.b[r.i] <= '9' {
			return r.errf("number with a leading zero")
		}
	} else if digits() == 0 {
		return r.errf("expected a number")
	}
	if r.peek('.') {
		r.i++
		if digits() == 0 {
			return r.errf("expected digits after '.'")
		}
	}
	if r.peek('e') || r.peek('E') {
		r.i++
		if r.peek('+') || r.peek('-') {
			r.i++
		}
		if digits() == 0 {
			return r.errf("expected digits in exponent")
		}
	}
	return nil
}

// skipValue walks over any value, including nested objects and arrays.
func (r *jreader) skipValue() error {
	r.skipWS()
	if r.i >= len(r.b) {
		return r.errf("unexpected end of input")
	}
	switch c := r.b[r.i]; {
	case c == '"':
		_, err := r.str()
		return err
	case c == '{':
		r.i++
		r.skipWS()
		if r.peek('}') {
			r.i++
			return nil
		}
		for {
			if _, err := r.str(); err != nil {
				return err
			}
			r.skipWS()
			if err := r.expect(':'); err != nil {
				return err
			}
			if err := r.skipValue(); err != nil {
				return err
			}
			r.skipWS()
			if r.peek(',') {
				r.i++
				r.skipWS()
				continue
			}
			return r.expect('}')
		}
	case c == '[':
		r.i++
		r.skipWS()
		if r.peek(']') {
			r.i++
			return nil
		}
		for {
			if err := r.skipValue(); err != nil {
				return err
			}
			r.skipWS()
			if r.peek(',') {
				r.i++
				r.skipWS()
				continue
			}
			return r.expect(']')
		}
	case c == 't':
		return r.word("true")
	case c == 'f':
		return r.word("false")
	case c == 'n':
		return r.word("null")
	default:
		return r.number()
	}
}

// object walks one object, handing each key to fn with the reader positioned on
// its value. fn either reads the value or skips it.
func (r *jreader) object(fn func(key string) error) error {
	r.skipWS()
	if err := r.expect('{'); err != nil {
		return err
	}
	r.skipWS()
	if r.peek('}') {
		r.i++
		return nil
	}
	for {
		key, err := r.str()
		if err != nil {
			return err
		}
		r.skipWS()
		if err := r.expect(':'); err != nil {
			return err
		}
		r.skipWS()
		if err := fn(key); err != nil {
			return err
		}
		r.skipWS()
		if r.peek(',') {
			r.i++
			r.skipWS()
			continue
		}
		if err := r.expect('}'); err != nil {
			return err
		}
		return nil
	}
}

// intoString reads a string value into dst, or skips it when it is something
// else (a package.json is free to have "version": null).
func (r *jreader) intoString(dst *string) error {
	if r.peek('"') {
		s, err := r.str()
		if err != nil {
			return err
		}
		*dst = s
		return nil
	}
	*dst = ""
	return r.skipValue()
}

func (r *jreader) intoStrings(dst *[]string) error {
	r.skipWS()
	if !r.peek('[') {
		return r.skipValue()
	}
	r.i++
	r.skipWS()
	if r.peek(']') {
		r.i++
		*dst = (*dst)[:0]
		return nil
	}
	out := (*dst)[:0]
	for {
		r.skipWS()
		if !r.peek('"') {
			if err := r.skipValue(); err != nil {
				return err
			}
		} else {
			s, err := r.str()
			if err != nil {
				return err
			}
			out = append(out, s)
		}
		r.skipWS()
		if r.peek(',') {
			r.i++
			continue
		}
		if err := r.expect(']'); err != nil {
			return err
		}
		*dst = out
		return nil
	}
}

// ---------- writer ----------

// appendJSONString appends s as a quoted JSON string. Only the escapes JSON
// requires are emitted; UTF-8 passes through.
func appendJSONString(b []byte, s string) []byte {
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			b = append(b, '\\', '"')
		case c == '\\':
			b = append(b, '\\', '\\')
		case c == '\n':
			b = append(b, '\\', 'n')
		case c == '\r':
			b = append(b, '\\', 'r')
		case c == '\t':
			b = append(b, '\\', 't')
		case c < 0x20:
			const hex = "0123456789abcdef"
			b = append(b, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
		default:
			b = append(b, c)
		}
	}
	return append(b, '"')
}
