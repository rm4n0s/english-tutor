package ui

import (
	"testing"

	"github.com/rm4n0s/english-tutor/internal/llm"
)

func TestFormatCount(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"}, {812, "812"}, {999, "999"}, {1000, "1.0k"}, {5234, "5.2k"}, {9949, "9.9k"}, {9950, "10k"}, {9999, "10k"},
		{10_000, "10k"}, {12_345, "12k"}, {12_500, "13k"}, {999_499, "999k"}, {999_500, "1.0M"}, {999_999, "1.0M"}, {1_000_000, "1.0M"}, {1_300_000, "1.3M"},
	}
	for _, tt := range tests {
		if got := FormatCount(tt.n); got != tt.want {
			t.Errorf("FormatCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFormatUsageAndTokens(t *testing.T) {
	tests := []struct {
		name          string
		last, session llm.Usage
		wantUsage     string
		wantTokens    string
	}{
		{
			name: "reported", last: llm.Usage{InputTokens: 812, OutputTokens: 164}, session: llm.Usage{InputTokens: 5234, OutputTokens: 1100},
			wantUsage:  "812 in / 164 out",
			wantTokens: "last: 812 in / 164 out · session: 5.2k in / 1.1k out",
		},
		{
			name: "estimated is marked", last: llm.Usage{InputTokens: 80, OutputTokens: 8, Estimated: true}, session: llm.Usage{InputTokens: 80, OutputTokens: 8, Estimated: true},
			wantUsage:  "~80 in / ~8 out",
			wantTokens: "last: ~80 in / ~8 out · session: ~80 in / ~8 out",
		},
		{name: "nothing yet", wantUsage: "0 in / 0 out", wantTokens: "last: 0 in / 0 out · session: 0 in / 0 out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatUsage(tt.last); got != tt.wantUsage {
				t.Errorf("FormatUsage() = %q, want %q", got, tt.wantUsage)
			}
			if got := FormatTokens(tt.last, tt.session); got != tt.wantTokens {
				t.Errorf("FormatTokens() = %q, want %q", got, tt.wantTokens)
			}
		})
	}
}

func TestFormatWords(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{{0, "0 words"}, {1, "1 word"}, {2, "2 words"}, {142, "142 words"}}
	for _, tt := range tests {
		if got := FormatWords(tt.n); got != tt.want {
			t.Errorf("FormatWords(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFormatStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   CheckStatus
		checked  bool
		issues   int
		unplaced int
		want     string
	}{
		{"before the first check", StatusIdle, false, 0, 0, "Ready"},
		{"checking", StatusChecking, true, 3, 0, "Checking..."},
		{"cancelled", StatusCancelled, true, 3, 0, "Check cancelled"},
		{"failed", StatusFailed, false, 0, 0, "Check failed"},
		{"discarded", StatusDiscarded, true, 0, 0, "The text changed while checking. Press Check again."},
		{"no errors", StatusIdle, true, 0, 0, "No errors found"},
		{"one issue", StatusIdle, true, 1, 0, "1 issue"},
		{"several issues", StatusIdle, true, 3, 0, "3 issues"},
		{"one could not be placed", StatusIdle, true, 3, 1, "3 issues · 1 issue could not be placed"},
		{"several could not be placed", StatusIdle, true, 0, 2, "No errors found · 2 issues could not be placed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatStatus(tt.status, tt.checked, tt.issues, tt.unplaced); got != tt.want {
				t.Errorf("FormatStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}
