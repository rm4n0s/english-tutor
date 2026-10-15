package store

import (
	"slices"
	"strings"
	"time"
)

// sortTypeStats orders rows by all-time count, highest first, then by error
// type so the order is stable.
func sortTypeStats(rows []TypeStat) {
	slices.SortStableFunc(rows, func(a, b TypeStat) int {
		if a.Total != b.Total {
			return b.Total - a.Total
		}
		return strings.Compare(a.ErrorType, b.ErrorType)
	})
}

// Summary is the line above the Statistics table.
type Summary struct {
	// Total is the number of mistakes in the period (in the category, if one
	// is chosen).
	Total int
	// MostFrequent is the error type with the most mistakes in the period, or
	// "" if there are none.
	MostFrequent string
}

// Summarize totals rows for a period and, if category is not empty, only for
// that category.
func Summarize(rows []TypeStat, p Period, category string) Summary {
	var s Summary
	best := 0
	for _, r := range rows {
		if category != "" && r.Category != category {
			continue
		}
		n := r.Count(p)
		s.Total += n
		if n > best || (n == best && n > 0 && r.ErrorType < s.MostFrequent) {
			best, s.MostFrequent = n, r.ErrorType
		}
	}
	return s
}

// sqliteTimeLayouts are the layouts the SQLite driver writes timestamps in.
// MAX(created_at) comes back as text, because the aggregate loses the
// column's declared type.
var sqliteTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05",
	time.RFC3339Nano,
}

func parseTime(s string) time.Time {
	for _, layout := range sqliteTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
