package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/english-tutor/internal/analysis"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// StatsColumn is a column of the mistakes table (section 4.6).
type StatsColumn int

// The columns, in the order they are shown.
const (
	ColType StatsColumn = iota
	ColCategory
	ColTotal
	ColLast7
	ColLast30
	ColLastSeen
	statsColumnCount
)

var statsColumnTitles = [statsColumnCount]string{"Error type", "Category", "Total", "Last 7 days", "Last 30 days", "Last seen"}

// StatsFilter is the category filter and the period selector of the dialog.
type StatsFilter struct {
	Period store.Period
	// Category is "" for all categories, or "grammar", "vocabulary", "syntax".
	Category string
}

// FilterStats keeps the error types that match the filter: the chosen category,
// and at least one mistake in the chosen period.
func FilterStats(rows []store.TypeStat, f StatsFilter) []store.TypeStat {
	var out []store.TypeStat
	for _, r := range rows {
		if f.Category != "" && r.Category != f.Category {
			continue
		}
		if r.Count(f.Period) == 0 {
			continue
		}
		out = append(out, r)
	}
	return out
}

// StatsSort says how the table is sorted.
type StatsSort struct {
	Column     StatsColumn
	Descending bool
}

// DefaultStatsSort is the order the dialog opens with: by Total, highest first.
var DefaultStatsSort = StatsSort{Column: ColTotal, Descending: true}

// SortStats sorts rows in place. Ties are broken by the error type's label, so
// the order is stable between refreshes. Clicking the same header again
// reverses the order; see NextSort.
func SortStats(rows []store.TypeStat, s StatsSort) {
	cmp := func(a, b store.TypeStat) int {
		switch s.Column {
		case ColCategory:
			return strings.Compare(a.Category, b.Category)
		case ColTotal:
			return a.Total - b.Total
		case ColLast7:
			return a.Last7 - b.Last7
		case ColLast30:
			return a.Last30 - b.Last30
		case ColLastSeen:
			return a.LastSeen.Compare(b.LastSeen)
		default:
			return strings.Compare(StatsTypeLabel(a.ErrorType), StatsTypeLabel(b.ErrorType))
		}
	}
	slices.SortStableFunc(rows, func(a, b store.TypeStat) int {
		c := cmp(a, b)
		if s.Descending {
			c = -c
		}
		if c != 0 {
			return c
		}
		return strings.Compare(StatsTypeLabel(a.ErrorType), StatsTypeLabel(b.ErrorType))
	})
}

// NextSort is the sort after a click on a column header: a new column starts
// with the larger values first (alphabetical for the text columns), the same
// column again reverses.
func NextSort(cur StatsSort, clicked StatsColumn) StatsSort {
	if cur.Column == clicked {
		return StatsSort{Column: clicked, Descending: !cur.Descending}
	}
	return StatsSort{Column: clicked, Descending: clicked != ColType && clicked != ColCategory}
}

// StatsTypeLabel returns the display name of an error type id. An id this
// version does not know (it may come from a newer one) is shown as it is.
func StatsTypeLabel(id string) string {
	if t, ok := analysis.Lookup(id); ok {
		return t.Label
	}
	return id
}

func statsCategoryLabel(category string) string { return analysis.Category(category).Label() }

// StatsCells returns the text of each cell of a row.
func StatsCells(r store.TypeStat) [statsColumnCount]string {
	return [statsColumnCount]string{
		ColType:     StatsTypeLabel(r.ErrorType),
		ColCategory: statsCategoryLabel(r.Category),
		ColTotal:    fmt.Sprintf("%d", r.Total),
		ColLast7:    fmt.Sprintf("%d", r.Last7),
		ColLast30:   fmt.Sprintf("%d", r.Last30),
		ColLastSeen: FormatDate(r.LastSeen),
	}
}

// FormatDate writes a time as a local calendar date, or "" for the zero time.
func FormatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02")
}

// PeriodLabel names a period for the selector and the summary.
func PeriodLabel(p store.Period) string {
	switch p {
	case Last30DaysPeriod:
		return "the last 30 days"
	case Last7DaysPeriod:
		return "the last 7 days"
	default:
		return "all time"
	}
}

// The store's periods, under names that read well here.
const (
	Last30DaysPeriod = store.Last30Days
	Last7DaysPeriod  = store.Last7Days
)

// StatsSummary is the line above the table: the number of mistakes in the
// selected period and category, and the most frequent error type.
func StatsSummary(rows []store.TypeStat, f StatsFilter) string {
	s := store.Summarize(rows, f.Period, f.Category)
	if s.Total == 0 {
		return "No mistakes recorded for " + PeriodLabel(f.Period) + "."
	}
	noun := "mistakes"
	if s.Total == 1 {
		noun = "mistake"
	}
	return fmt.Sprintf("%d %s in %s. Most frequent: %s.", s.Total, noun, PeriodLabel(f.Period), StatsTypeLabel(s.MostFrequent))
}

// FormatExample is one line of the examples list: the user's own wrong text and
// the day it was written.
func FormatExample(e store.Example) string {
	snippet := strings.Join(strings.Fields(e.Snippet), " ")
	if d := FormatDate(e.CreatedAt); d != "" {
		return fmt.Sprintf("%s  (%s)", snippet, d)
	}
	return snippet
}

// UsageRow is one line of the Token usage table: one LLM, three periods.
type UsageRow struct {
	LLM string
	// Cells is "in / out" for all time, 30 days and 7 days.
	AllTime, Last30, Last7 string
}

// UsageRows joins the totals of the three periods by LLM name. An LLM with no
// requests in a period shows "0 in / 0 out" there; counts that include
// estimates are marked with "~" (section 5.8).
func UsageRows(all, last30, last7 []store.UsageTotal) []UsageRow {
	byName := map[string]*UsageRow{}
	var names []string
	add := func(list []store.UsageTotal, set func(r *UsageRow, text string)) {
		for _, t := range list {
			r, ok := byName[t.LLMName]
			if !ok {
				r = &UsageRow{LLM: t.LLMName, AllTime: zeroUsage, Last30: zeroUsage, Last7: zeroUsage}
				byName[t.LLMName] = r
				names = append(names, t.LLMName)
			}
			mark := ""
			if t.Estimated {
				mark = "~"
			}
			set(r, fmt.Sprintf("%s%s in / %s%s out", mark, FormatCount(t.InputTokens), mark, FormatCount(t.OutputTokens)))
		}
	}
	add(all, func(r *UsageRow, s string) { r.AllTime = s })
	add(last30, func(r *UsageRow, s string) { r.Last30 = s })
	add(last7, func(r *UsageRow, s string) { r.Last7 = s })
	slices.Sort(names)
	out := make([]UsageRow, len(names))
	for i, n := range names {
		out[i] = *byName[n]
	}
	return out
}

const zeroUsage = "0 in / 0 out"
