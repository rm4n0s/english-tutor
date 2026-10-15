// Package ui is the Qt user interface. The files without Qt imports (colors.go,
// format.go) hold the logic that can be tested without a display.
package ui

import "github.com/rm4n0s/english-tutor/internal/analysis"

// UnderlineStyle is how the editor underlines the errors of a category, so
// that errors can be told apart by more than colour (section 4.2).
type UnderlineStyle int

// The underline styles.
const (
	UnderlineWave UnderlineStyle = iota
	UnderlineDash
	UnderlineDot
)

type categoryLook struct {
	light, dark string
	style       UnderlineStyle
	sample      string
}

// The Okabe-Ito colour-blind-safe palette (section 4.2 of the spec).
var looks = map[analysis.Category]categoryLook{
	analysis.Grammar:    {light: "#D55E00", dark: "#FF8A4C", style: UnderlineWave, sample: "~~"},
	analysis.Vocabulary: {light: "#0072B2", dark: "#56B4E9", style: UnderlineDash, sample: "--"},
	analysis.Syntax:     {light: "#009E73", dark: "#2FD3A2", style: UnderlineDot, sample: ".."},
}

// CategoryColor returns the colour of a category as #RRGGBB, for a light or a
// dark theme. An unknown category gets a neutral grey.
func CategoryColor(c analysis.Category, dark bool) string {
	l, ok := looks[c]
	switch {
	case !ok:
		return "#808080"
	case dark:
		return l.dark
	default:
		return l.light
	}
}

// CategoryStyle returns how the category is underlined.
func CategoryStyle(c analysis.Category) UnderlineStyle { return looks[c].style }

// CategorySample returns the short text sample that stands for the underline
// in the legend ("~~", "--", "..").
func CategorySample(c analysis.Category) string {
	if l, ok := looks[c]; ok {
		return l.sample
	}
	return "??"
}
