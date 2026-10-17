package analysis

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// Limits on what a reply may contribute. A model that ignores the contract
// cannot make the UI show a huge tooltip or keep a huge snippet.
const (
	maxMessageRunes = 300
	maxContextRunes = 200
	maxSnippetRunes = 2000
)

// Issue is one error reported by the model, validated but not yet placed in
// the text.
type Issue struct {
	// Text is the faulty snippet, exactly as the model copied it.
	Text string
	Type ErrorType
	// Message is the one-line summary shown as the tooltip.
	Message       string
	ContextBefore string
	ContextAfter  string
}

// Parsed is the result of parsing one reply.
type Parsed struct {
	Issues []Issue
	// Dropped counts entries that were unusable (no text).
	Dropped int
}

type rawIssue struct {
	Text          string `json:"text"`
	ErrorType     string `json:"error_type"`
	Message       string `json:"message"`
	ContextBefore string `json:"context_before"`
	ContextAfter  string `json:"context_after"`
}

type rawResponse struct {
	Issues *[]rawIssue `json:"issues"`
}

// ParseResponse parses the model's reply. It is tolerant about packaging:
// markdown code fences and text before or after the JSON are ignored, and a
// bare JSON array of issues is accepted. It is strict about content: the JSON
// must contain an "issues" list. Entries without text are dropped and counted,
// unknown error types are mapped by Resolve, and over-long fields are cut.
// Anything else gives LLMResponseInvalid.
func ParseResponse(reply string) (Parsed, error) {
	raw, ok := decodeIssues(reply)
	if !ok {
		return Parsed{}, errors.New("LLMResponseInvalid",
			"The model's answer was not in the expected format.",
			"reply_bytes", len(reply))
	}
	var out Parsed
	for _, r := range raw {
		text := strings.TrimSpace(r.Text)
		if text == "" || utf8.RuneCountInString(text) > maxSnippetRunes {
			out.Dropped++
			continue
		}
		typ := Resolve(r.ErrorType)
		msg := strings.TrimSpace(r.Message)
		if msg == "" {
			msg = typ.Label
		}
		out.Issues = append(out.Issues, Issue{
			// Surrounding whitespace is not part of the faulty snippet, and
			// the locator needs the snippet exactly as it is in the text.
			Text:          text,
			Type:          typ,
			Message:       truncateRunes(msg, maxMessageRunes),
			ContextBefore: tailRunes(r.ContextBefore, maxContextRunes),
			ContextAfter:  truncateRunes(r.ContextAfter, maxContextRunes),
		})
	}
	return out, nil
}

// thinkEnd closes the thinking block some reasoning models put before the answer.
const thinkEnd = "</think>"

// maxStarts bounds how many '{' positions decodeIssues tries, so a long reply
// full of braces cannot make parsing quadratic.
const maxStarts = 8

// decodeIssues finds the issue list in reply. It tries the first few '{' as the
// start of the contract object, so a reply like "Sure! {...}" still works. A
// bare array is accepted only when it is the whole reply (after fences),
// because an array nested in some other object is not an issue list.
func decodeIssues(reply string) ([]rawIssue, bool) {
	s := strings.TrimPrefix(reply, "\xef\xbb\xbf")
	// A reasoning model may put its thinking in the answer. Only what follows
	// it is the answer; the thinking can mention braces and draft other JSON.
	if i := strings.LastIndex(s, thinkEnd); i >= 0 {
		s = s[i+len(thinkEnd):]
	}
	tried := 0
	for start := 0; start < len(s) && tried < maxStarts; start++ {
		if s[start] != '{' {
			continue
		}
		tried++
		var r rawResponse
		if decodeAt(s[start:], &r) && r.Issues != nil {
			return *r.Issues, true
		}
	}
	if body := stripFences(s); strings.HasPrefix(body, "[") {
		var list []rawIssue
		if decodeAt(body, &list) && list != nil {
			return list, true
		}
	}
	return nil, false
}

// stripFences trims space and a surrounding markdown code fence.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		} else {
			s = ""
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

func decodeAt(s string, v any) bool {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	return dec.Decode(v) == nil
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
