// Package jsonx holds the lenient JSON decoding helpers the unicom360 packages share.
//
// They are lenient on purpose: a field of an unexpected JSON type is left at its zero value
// rather than failing the whole response, and every model keeps the raw JSON it came from.
package jsonx

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// String decodes a JSON string as is and a number or boolean as its literal text
// (message_id can arrive as a number). null and other values give "".
type String string

// UnmarshalJSON implements json.Unmarshaler.
func (s *String) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		*s = ""
		return nil
	}
	switch c := b[0]; {
	case c == '"':
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = String(v)
	case c == '-' || (c >= '0' && c <= '9') || bytes.Equal(b, []byte("true")) || bytes.Equal(b, []byte("false")):
		*s = String(b)
	default:
		*s = ""
	}
	return nil
}

// Int decodes a JSON number (or a string holding a whole number). Anything else gives nil.
type Int struct{ V *int }

// UnmarshalJSON implements json.Unmarshaler.
func (n *Int) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	n.V = nil
	if len(b) == 0 {
		return nil
	}
	text := string(b)
	if b[0] == '"' {
		if json.Unmarshal(b, &text) != nil {
			return nil
		}
	}
	if i, err := strconv.Atoi(text); err == nil {
		n.V = &i
		return nil
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil && f == float64(int(f)) {
		i := int(f)
		n.V = &i
	}
	return nil
}

// Value returns the number, or 0 when there was none.
func (n Int) Value() int {
	if n.V == nil {
		return 0
	}
	return *n.V
}

// Bool decodes true or false. Anything else gives nil.
type Bool struct{ V *bool }

// UnmarshalJSON implements json.Unmarshaler.
func (f *Bool) UnmarshalJSON(b []byte) error {
	f.V = BoolValue(b)
	return nil
}

// Strings decodes a list of strings, each element through String. A value that is not a list
// gives nil; an empty list gives an empty, non-nil slice.
type Strings []string

// UnmarshalJSON implements json.Unmarshaler.
func (l *Strings) UnmarshalJSON(b []byte) error {
	*l = nil
	if !IsArray(b) {
		return nil
	}
	var items []String
	if json.Unmarshal(b, &items) != nil {
		return nil
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = string(it)
	}
	*l = out
	return nil
}

// FirstByte returns the first non-space byte of b, or 0.
func FirstByte(b []byte) byte {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return 0
	}
	return b[0]
}

// IsObject reports whether b is (starts like) a JSON object.
func IsObject(b []byte) bool { return FirstByte(b) == '{' }

// IsArray reports whether b is (starts like) a JSON array.
func IsArray(b []byte) bool { return FirstByte(b) == '[' }

// Clone copies b (nil stays nil).
func Clone(b []byte) json.RawMessage {
	if b == nil {
		return nil
	}
	return append(json.RawMessage(nil), b...)
}

// RawOrNil returns a trimmed copy of b, or nil when b is empty or JSON null.
func RawOrNil(b []byte) json.RawMessage {
	t := bytes.TrimSpace(b)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		return nil
	}
	return Clone(t)
}

// DecodeObject decodes b into v when b is a JSON object, and reports whether it did.
func DecodeObject(b []byte, v any) bool {
	return IsObject(b) && json.Unmarshal(b, v) == nil
}

// StringValue returns raw's value when it is a JSON string, else "".
func StringValue(raw []byte) string {
	if FirstByte(raw) != '"' {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// BoolValue returns raw's value when it is true or false, else nil.
func BoolValue(raw []byte) *bool {
	switch string(bytes.TrimSpace(raw)) {
	case "true":
		t := true
		return &t
	case "false":
		f := false
		return &f
	}
	return nil
}

// Marshal encodes v without escaping <, > and &, so text (an HTML email body, a message) goes
// out exactly as given.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
