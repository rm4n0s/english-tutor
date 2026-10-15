package ui

import (
	"fmt"

	"github.com/rm4n0s/english-tutor/internal/llm"
)

// FormatCount writes a token count compactly: 812, 5.2k, 12k, 1.3M.
func FormatCount(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 9950: // 9.95k and up would round to "10.0k"
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	case n < 999_500: // 999.5k and up would round to "1000k"
		return fmt.Sprintf("%dk", (n+500)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
}

// FormatUsage writes "812 in / 164 out", with a "~" in front of each number
// that was estimated rather than reported by the provider (section 5.8).
func FormatUsage(u llm.Usage) string {
	mark := ""
	if u.Estimated {
		mark = "~"
	}
	return fmt.Sprintf("%s%s in / %s%s out", mark, FormatCount(u.InputTokens), mark, FormatCount(u.OutputTokens))
}

// FormatTokens is the token part of the status bar:
// "last: 812 in / 164 out · session: 5.2k in / 1.1k out".
func FormatTokens(last, session llm.Usage) string {
	return fmt.Sprintf("last: %s · session: %s", FormatUsage(last), FormatUsage(session))
}

// FormatWords writes the word count: "1 word", "142 words".
func FormatWords(n int) string {
	if n == 1 {
		return "1 word"
	}
	return fmt.Sprintf("%d words", n)
}

// CheckStatus is the state of the last check, for the status bar.
type CheckStatus int

// The check states.
const (
	StatusIdle CheckStatus = iota
	StatusChecking
	StatusCancelled
	StatusFailed
	StatusDiscarded
)

// FormatStatus is the left part of the status bar (section 4.1). checked says
// whether a check has finished; issues is the number of issues placed and
// unplaced the number the locator could not place.
func FormatStatus(s CheckStatus, checked bool, issues, unplaced int) string {
	switch s {
	case StatusChecking:
		return "Checking..."
	case StatusCancelled:
		return "Check cancelled"
	case StatusFailed:
		return "Check failed"
	case StatusDiscarded:
		return "The text changed while checking. Press Check again."
	}
	if !checked {
		return "Ready"
	}
	var text string
	switch issues {
	case 0:
		text = "No errors found"
	case 1:
		text = "1 issue"
	default:
		text = fmt.Sprintf("%d issues", issues)
	}
	switch unplaced {
	case 0:
	case 1:
		text += " · 1 issue could not be placed"
	default:
		text += fmt.Sprintf(" · %d issues could not be placed", unplaced)
	}
	return text
}
