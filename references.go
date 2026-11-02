package unicom360

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// newReferenceID returns "u360-" and 32 lowercase hex characters: a random UUID v4 without
// its dashes.
func newReferenceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on supported platforms; a fixed fallback would defeat
		// de-duplication, so fail loudly.
		panic("unicom360: crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return "u360-" + hex.EncodeToString(b[:])
}

// hasReference reports whether ref holds at least one non-whitespace character, which is
// what makes a send safe to retry.
func hasReference(ref string) bool { return strings.TrimSpace(ref) != "" }
