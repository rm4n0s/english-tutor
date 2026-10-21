package view

import (
	"net/url"
	"strings"
	"testing"
)

func TestControlsFor(t *testing.T) {
	tests := []struct {
		name  string
		state ExplainState
		want  ExplainControls
	}{
		{"idle shows only the hint", ExplainIdle, ExplainControls{}},
		{"loading", ExplainLoading, ExplainControls{Header: true, Progress: true, Cancel: true, NotAnError: true}},
		{"ready", ExplainReady, ExplainControls{Header: true, Redo: true, RedoLabel: "Regenerate", NotAnError: true}},
		{"failed", ExplainFailed, ExplainControls{Header: true, Redo: true, RedoLabel: "Retry", NotAnError: true}},
		{"cancelled", ExplainCancelled, ExplainControls{Header: true, Redo: true, RedoLabel: "Regenerate", NotAnError: true}},
		{"unknown state is idle", ExplainState(99), ExplainControls{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ControlsFor(tt.state); got != tt.want {
				t.Errorf("ControlsFor() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFormatExplainHeader(t *testing.T) {
	tests := []struct {
		name         string
		total, last7 int
		counted      bool
		want         string
	}{
		{"several", 7, 2, true, "Verb tense: you have made this mistake 7 times (2 in the last 7 days)"},
		{"once", 1, 1, true, "Verb tense: you have made this mistake 1 time (1 in the last 7 days)"},
		{"never recorded", 0, 0, true, "Verb tense: no mistake of this type recorded yet"},
		{"unrecorded issue", 3, 0, false, "Verb tense: you have made this mistake 3 times (0 in the last 7 days). This one is not counted: only the Check button counts."},
		{"unrecorded and never recorded", 0, 0, false, "Verb tense: no mistake of this type recorded yet. This one is not counted: only the Check button counts."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatExplainHeader("Verb tense", tt.total, tt.last7, tt.counted); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestDecideLink(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		inHint bool
		want   LinkAction
	}{
		{"https is confirmed", "https://example.com/rule", false, LinkConfirm},
		{"http is confirmed", "http://example.com", false, LinkConfirm},
		{"scheme case does not matter", "HTTPS://Example.com", false, LinkConfirm},
		{"surrounding space is ignored", "  https://example.com  ", false, LinkConfirm},
		{"user info trick is confirmed, the dialog shows the whole address", "https://good.com@evil.example/", false, LinkConfirm},
		{"no host", "https://", false, LinkIgnore},
		{"http without slashes has no host", "http:example.com", false, LinkIgnore},
		{"file", "file:///etc/passwd", false, LinkIgnore},
		{"javascript", "javascript:alert(1)", false, LinkIgnore},
		{"data", "data:text/html;base64,PHNjcmlwdD4=", false, LinkIgnore},
		{"ftp", "ftp://example.com/x", false, LinkIgnore},
		{"mailto", "mailto:a@b.c", false, LinkIgnore},
		{"relative", "/path", false, LinkIgnore},
		{"fragment", "#top", false, LinkIgnore},
		{"empty", "", false, LinkIgnore},
		{"garbage", "ht!tp://%zz", false, LinkIgnore},
		{"preferences in the hint", "preferences:", true, LinkPreferences},
		{"preferences in an explanation is ignored", "preferences:", false, LinkIgnore},
		{"https in the hint is still confirmed", "https://example.com", true, LinkConfirm},
		{"preferences with a path", "preferences:anything", true, LinkPreferences},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideLink(tt.raw, tt.inHint); got != tt.want {
				t.Errorf("DecideLink(%q, %v) = %v, want %v", tt.raw, tt.inHint, got, tt.want)
			}
		})
	}
}

func FuzzDecideLink(f *testing.F) {
	for _, s := range []string{"https://a.b", "javascript:x", "preferences:", "", "http://", "\x00", "https://a b"} {
		f.Add(s, true)
		f.Add(s, false)
	}
	f.Fuzz(func(t *testing.T, raw string, inHint bool) {
		if DecideLink(raw, inHint) != LinkConfirm {
			return
		}
		// Whatever was accepted must really be an http(s) link with a host.
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			t.Fatalf("accepted a link that does not parse: %q", raw)
		}
		if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
			t.Fatalf("accepted a link with scheme %q: %q", u.Scheme, raw)
		}
		if u.Host == "" {
			t.Fatalf("accepted a link without a host: %q", raw)
		}
	})
}
