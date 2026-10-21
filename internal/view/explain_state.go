package view

import (
	"fmt"
	"net/url"
	"strings"
)

// ExplainState is what the explanation panel shows (section 4.4).
type ExplainState int

// The states of the explanation panel.
const (
	// ExplainIdle shows a hint (or the no-LLM message).
	ExplainIdle ExplainState = iota
	// ExplainLoading shows the selected issue while the explanation streams in.
	ExplainLoading
	// ExplainReady shows the markdown explanation.
	ExplainReady
	// ExplainFailed shows the error and a Retry button.
	ExplainFailed
	// ExplainCancelled is the idle look after the user cancelled, with a
	// Regenerate button.
	ExplainCancelled
)

// ExplainControls says which parts of the panel are visible in a state.
type ExplainControls struct {
	Header     bool   // the line with the error type and the count
	Progress   bool   // the busy indicator
	Cancel     bool   // the Cancel button
	Redo       bool   // the Regenerate / Retry button
	RedoLabel  string // its text
	NotAnError bool
}

func ControlsFor(s ExplainState) ExplainControls {
	switch s {
	case ExplainLoading:
		return ExplainControls{Header: true, Progress: true, Cancel: true, NotAnError: true}
	case ExplainReady:
		return ExplainControls{Header: true, Redo: true, RedoLabel: "Regenerate", NotAnError: true}
	case ExplainFailed:
		return ExplainControls{Header: true, Redo: true, RedoLabel: "Retry", NotAnError: true}
	case ExplainCancelled:
		return ExplainControls{Header: true, Redo: true, RedoLabel: "Regenerate", NotAnError: true}
	default:
		return ExplainControls{}
	}
}

// FormatExplainHeader is the line above the explanation: the error type and
// how often the user has made it (section 4.4), for example
// "Subject-verb agreement: you have made this mistake 7 times (2 in the last 7
// days)". counted is false for an issue that was found by auto-check and so is
// not in the statistics; the line then says so, because the numbers exclude it.
func FormatExplainHeader(label string, total, last7 int, counted bool) string {
	var line string
	switch total {
	case 0:
		line = label + ": no mistake of this type recorded yet"
	case 1:
		line = fmt.Sprintf("%s: you have made this mistake 1 time (%d in the last 7 days)", label, last7)
	default:
		line = fmt.Sprintf("%s: you have made this mistake %d times (%d in the last 7 days)", label, total, last7)
	}
	if !counted {
		line += ". This one is not counted: only the Check button counts."
	}
	return line
}

// LinkAction is what to do with a link clicked in the explanation panel.
type LinkAction int

// The possible actions.
const (
	// LinkIgnore: do nothing. The link is not opened.
	LinkIgnore LinkAction = iota
	// LinkPreferences: open the Preferences dialog.
	LinkPreferences
	// LinkConfirm: ask the user, showing the full address, before opening it
	// in the browser.
	LinkConfirm
)

// DecideLink decides what a clicked link does (section 5.9). Explanations
// come from an LLM, so a link in them is untrusted:
//
//   - only http and https links with a host are ever offered, and only after a
//     confirmation that shows the full address;
//   - every other scheme (file:, javascript:, data:, ftp:, mailto: ...) is
//     ignored;
//   - the preferences: link is followed only in the panel's own hint, never in
//     an explanation, where an LLM could have written it.
func DecideLink(raw string, inHint bool) LinkAction {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return LinkIgnore
	}
	switch strings.ToLower(u.Scheme) {
	case "preferences":
		if inHint {
			return LinkPreferences
		}
	case "http", "https":
		if u.Host != "" {
			return LinkConfirm
		}
	}
	return LinkIgnore
}
