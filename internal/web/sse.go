package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// stream writes Server-Sent Events in the form the Datastar client reads
// (datastar-patch-elements and datastar-patch-signals).
type stream struct {
	w  io.Writer
	rc *http.ResponseController
}

// newStream prepares w for events. It fails when the connection cannot flush.
func newStream(w http.ResponseWriter) (*stream, error) {
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return nil, err
	}
	return &stream{w: w, rc: rc}, nil
}

// patchElements replaces the element whose id is in html (morphing it in
// place). With a selector the html goes inside the first match instead.
func (s *stream) patchElements(html string) error {
	return s.event("datastar-patch-elements", "elements", html)
}

// patchSignals sets signals on the page; values are anything encoding/json takes.
func (s *stream) patchSignals(signals map[string]any) error {
	b, err := json.Marshal(signals)
	if err != nil {
		return err
	}
	return s.event("datastar-patch-signals", "signals", string(b))
}

func (s *stream) event(name, key, payload string) error {
	return s.eventWith(name, nil, key, payload)
}

// eventWith writes one event. Every line of the payload is its own data line,
// which the client joins again with newlines.
func (s *stream) eventWith(name string, options []string, key, payload string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "event: %s\n", name)
	for _, o := range options {
		fmt.Fprintf(&b, "data: %s\n", o)
	}
	for _, line := range strings.Split(payload, "\n") {
		fmt.Fprintf(&b, "data: %s %s\n", key, strings.TrimSuffix(line, "\r"))
	}
	b.WriteString("\n")
	if _, err := io.WriteString(s.w, b.String()); err != nil {
		return err
	}
	return s.rc.Flush()
}

// comment writes an SSE comment, which keeps an idle connection open.
func (s *stream) comment(text string) error {
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		return err
	}
	return s.rc.Flush()
}
