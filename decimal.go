package unicom360

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// Decimal is a balance exactly as the gateway sent it: the JSON number's own text, for example
// "1250" or "37.5". Keeping the text avoids the rounding a float64 would add; convert it with
// Float64, Int64, Number (a json.Number), or a decimal library of your choice through String.
//
// The empty Decimal means the value was absent or null.
type Decimal string

// String returns the number's text as received.
func (d Decimal) String() string { return string(d) }

// Number returns the value as a json.Number.
func (d Decimal) Number() json.Number { return json.Number(d) }

// Float64 returns the value as a float64.
func (d Decimal) Float64() (float64, error) { return strconv.ParseFloat(string(d), 64) }

// Int64 returns the value as an int64. It fails for a fractional value.
func (d Decimal) Int64() (int64, error) { return strconv.ParseInt(string(d), 10, 64) }

// UnmarshalJSON keeps a JSON number's text. A string is kept as its content; null, and any
// other JSON value, gives the empty Decimal (the model's Raw still has it).
func (d *Decimal) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case len(b) == 0:
		*d = ""
	case b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*d = Decimal(s)
	case b[0] == '-' || (b[0] >= '0' && b[0] <= '9'):
		*d = Decimal(b)
	default:
		*d = ""
	}
	return nil
}

// MarshalJSON writes the value as a JSON number (null when empty).
func (d Decimal) MarshalJSON() ([]byte, error) {
	if d == "" {
		return []byte("null"), nil
	}
	return json.Marshal(json.Number(d))
}
