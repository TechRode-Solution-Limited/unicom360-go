// Package gsm counts and checks SMS text the way the Unicom360 gateway does.
//
// The gateway accepts only GSM-7 text: the GSM 03.38 basic set plus the extension characters
// ^{}\[~]|€ (and no form feed), at most 1,200 characters. A CR LF pair counts as one
// character. Each recipient is billed max(1, ceil(length / 160)) units: these are billing
// units, not GSM segments.
//
// The helpers are opt-in: the SDK never checks a message itself, it lets the gateway decide.
// Use them to show a character counter, warn before sending, or replace characters the gateway
// would refuse.
package gsm

import (
	"strings"
	"unicode/utf8"
)

// Charset is every character the gateway accepts in an SMS, exactly as its counter
// (sms_part_counter 0.1.7) defines GSM-7: the basic set, then the extension characters.
const Charset = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà^{}\\[~]|€"

// MaxLength is the longest message the gateway accepts, in characters (Length).
const MaxLength = 1200

// UnitLength is the number of characters billed as one unit.
const UnitLength = 160

// Issue is a reason ValidateSMS gives for refusing a text.
type Issue string

// The issues ValidateSMS reports, in this order.
const (
	// IssueEmpty: the text is empty or only spaces, tabs, CR and LF.
	IssueEmpty Issue = "empty"
	// IssueTooLong: Length is over MaxLength.
	IssueTooLong Issue = "too_long"
	// IssueNonGSM: the text has a character outside Charset.
	IssueNonGSM Issue = "non_gsm"
)

// NonGSMCharacter is a character outside Charset and its position.
type NonGSMCharacter struct {
	// Char is the character.
	Char rune
	// Index is its 0-based position in code points (a CR LF pair counts as two here).
	Index int
}

var charset = func() map[rune]bool {
	m := make(map[rune]bool, utf8.RuneCountInString(Charset))
	for _, r := range Charset {
		m[r] = true
	}
	return m
}()

// IsGSM7 reports whether every character of text is in Charset. The empty text is GSM-7.
func IsGSM7(text string) bool {
	for _, r := range text {
		if !charset[r] {
			return false
		}
	}
	return true
}

// Length returns the length the gateway counts: code points, with each CR LF pair counted as
// one. An emoji is one character.
func Length(text string) int {
	return utf8.RuneCountInString(text) - strings.Count(text, "\r\n")
}

// Units returns the billing units for text: max(1, ceil(Length / 160)).
func Units(text string) int {
	n := Length(text)
	if n <= UnitLength {
		return 1
	}
	return (n + UnitLength - 1) / UnitLength
}

// NonGSMCharacters returns every character of text outside Charset, in order. It returns nil
// for GSM-7 text.
func NonGSMCharacters(text string) []NonGSMCharacter {
	var out []NonGSMCharacter
	i := 0
	for _, r := range text {
		if !charset[r] {
			out = append(out, NonGSMCharacter{Char: r, Index: i})
		}
		i++
	}
	return out
}

// ValidateSMS returns the reasons the gateway would refuse text, in the order empty, too_long,
// non_gsm. An empty result means the gateway would accept it.
func ValidateSMS(text string) []Issue {
	var issues []Issue
	if strings.Trim(text, " \t\r\n") == "" {
		issues = append(issues, IssueEmpty)
	}
	if Length(text) > MaxLength {
		issues = append(issues, IssueTooLong)
	}
	if !IsGSM7(text) {
		issues = append(issues, IssueNonGSM)
	}
	return issues
}
