package view

import (
	"regexp"
	"testing"

	"github.com/rm4n0s/english-tutor/internal/analysis"
)

var hexColor = regexp.MustCompile(`^#[0-9A-F]{6}$`)

func TestCategoryColors(t *testing.T) {
	tests := []struct {
		c           analysis.Category
		light, dark string
		style       UnderlineStyle
		sample      string
	}{
		{analysis.Grammar, "#D55E00", "#FF8A4C", UnderlineWave, "~~"},
		{analysis.Vocabulary, "#0072B2", "#56B4E9", UnderlineDash, "--"},
		{analysis.Syntax, "#009E73", "#2FD3A2", UnderlineDot, ".."},
	}
	seenColors := map[string]bool{}
	seenStyles := map[UnderlineStyle]bool{}
	for _, tt := range tests {
		t.Run(string(tt.c), func(t *testing.T) {
			if got := CategoryColor(tt.c, false); got != tt.light {
				t.Errorf("light = %s, want %s", got, tt.light)
			}
			if got := CategoryColor(tt.c, true); got != tt.dark {
				t.Errorf("dark = %s, want %s", got, tt.dark)
			}
			if CategoryStyle(tt.c) != tt.style || CategorySample(tt.c) != tt.sample {
				t.Errorf("style/sample = %v/%q", CategoryStyle(tt.c), CategorySample(tt.c))
			}
		})
		seenColors[tt.light], seenColors[tt.dark] = true, true
		seenStyles[tt.style] = true
	}
	if len(seenColors) != 6 || len(seenStyles) != 3 {
		t.Error("every category needs its own colour and underline style, so errors differ by more than colour")
	}
}

func TestEveryCategoryHasALook(t *testing.T) {
	for _, c := range analysis.Categories() {
		for _, dark := range []bool{false, true} {
			if got := CategoryColor(c, dark); !hexColor.MatchString(got) {
				t.Errorf("CategoryColor(%s, %v) = %q", c, dark, got)
			}
		}
	}
}

func TestUnknownCategory(t *testing.T) {
	if got := CategoryColor("nope", false); got != "#808080" {
		t.Errorf("color = %s", got)
	}
	if CategorySample("nope") != "??" {
		t.Errorf("sample = %s", CategorySample("nope"))
	}
}
