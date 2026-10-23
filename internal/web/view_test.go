package web

import (
	"testing"

	"github.com/rm4n0s/english-tutor/internal/store"
)

func TestCSSFontFamily(t *testing.T) {
	tests := []struct {
		name, family, want string
	}{
		{"default", "", "var(--ui-font)"},
		{"plain", "DejaVu Serif", `"DejaVu Serif", var(--ui-font)`},
		{"quote and backslash", `A"B\C`, `"A\"B\\C", var(--ui-font)`},
		{"control characters", "A\nB\x7f", `"A\a B\7f ", var(--ui-font)`},
		{"cannot end the declaration", `x"; color: red; --y: "`, `"x\"; color: red; --y: \"", var(--ui-font)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cssFontFamily(tt.family); got != tt.want {
				t.Errorf("cssFontFamily(%q) = %s, want %s", tt.family, got, tt.want)
			}
		})
	}
}

func TestFontCSS(t *testing.T) {
	tests := []struct {
		name  string
		fonts store.Fonts
		want  string
	}{
		{"defaults", store.DefaultFonts(),
			"--editor-font: var(--ui-font); --editor-size: 13pt; --button-font: var(--ui-font); --button-size: 11pt; " +
				"--results-font: var(--ui-font); --results-size: 11pt;"},
		{"chosen", store.Fonts{Editor: store.Font{Family: "Noto Serif", Size: 16}, Buttons: store.Font{Size: 9}, Results: store.Font{Family: "Mono", Size: 12}},
			`--editor-font: "Noto Serif", var(--ui-font); --editor-size: 16pt; --button-font: var(--ui-font); --button-size: 9pt; ` +
				`--results-font: "Mono", var(--ui-font); --results-size: 12pt;`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fontCSS(tt.fonts); got != tt.want {
				t.Errorf("fontCSS() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}
