package analysis

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// NormaliseSnippet lower-cases a snippet and collapses every run of whitespace
// into one space, so that trivial differences do not make a different mistake.
func NormaliseSnippet(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// Fingerprint identifies one mistake within one text (section 5.7): the
// SHA-256 of the normalised snippet text[start:end] and its occurrence index,
// the number of times the same snippet (ignoring case) occurs in the text
// before start. Nothing that comes from the model's own judgement goes into it
// (no error type, context or position), because the model can answer
// differently on every run.
func Fingerprint(text string, start, end int) string {
	snippet := text[start:end]
	occurrence := 0
	if snippet != "" {
		occurrence = strings.Count(strings.ToLower(text[:start]), strings.ToLower(snippet))
	}
	sum := sha256.Sum256([]byte(NormaliseSnippet(snippet) + "\x00" + strconv.Itoa(occurrence)))
	return hex.EncodeToString(sum[:])
}
