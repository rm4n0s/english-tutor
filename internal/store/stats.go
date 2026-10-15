package store

import (
	"context"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Period selects how far back statistics are counted.
type Period int

// The periods offered by the Statistics dialog.
const (
	AllTime Period = iota
	Last30Days
	Last7Days
)

// since returns the start of the period relative to now; ok is false for AllTime.
func (p Period) since(now time.Time) (time.Time, bool) {
	switch p {
	case Last30Days:
		return now.AddDate(0, 0, -30), true
	case Last7Days:
		return now.AddDate(0, 0, -7), true
	default:
		return time.Time{}, false
	}
}

// Mistake is one placed issue to be recorded after a manual check.
type Mistake struct {
	ErrorType   string
	Category    string
	Snippet     string
	Fingerprint string
}

type ignoreKey struct{ source, fingerprint string }

// Stats records and counts mistakes and token usage. Its only state is the
// session's "not an error" ignore set; everything else is in the database.
type Stats struct {
	store *Store
	clock func() time.Time

	mu      sync.Mutex
	ignored map[ignoreKey]struct{}
}

// NewStats returns a Stats over s. clock supplies "now" (nil means time.Now),
// so tests can control the 7 and 30 day windows.
func NewStats(s *Store, clock func() time.Time) *Stats {
	if clock == nil {
		clock = time.Now
	}
	return &Stats{store: s, clock: clock, ignored: map[ignoreKey]struct{}{}}
}

// RecordResult says what Record did.
type RecordResult struct {
	// Inserted is the number of new mistakes counted.
	Inserted int
	// Duplicates were already recorded for this source (a re-check).
	Duplicates int
	// Ignored were marked "not an error" earlier in this session.
	Ignored int
}

// batchSize keeps an insert below SQLite's limit on bound variables.
const batchSize = 100

// Record stores the mistakes of a manual check of the text identified by
// sourceKey. A mistake already recorded for the same source (same fingerprint)
// is not counted again, and one marked "not an error" is skipped.
func (st *Stats) Record(ctx context.Context, sourceKey, llmName string, mistakes []Mistake) (RecordResult, error) {
	var res RecordResult
	now := st.clock()
	events := make([]ErrorEvent, 0, len(mistakes))
	for _, m := range mistakes {
		if st.isIgnored(sourceKey, m.Fingerprint) {
			res.Ignored++
			continue
		}
		events = append(events, ErrorEvent{
			ErrorType:   m.ErrorType,
			Category:    m.Category,
			Snippet:     m.Snippet,
			SourceKey:   sourceKey,
			Fingerprint: m.Fingerprint,
			LLMName:     llmName,
			CreatedAt:   now,
		})
	}
	if len(events) == 0 {
		return res, nil
	}
	tx := st.store.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(&events, batchSize)
	if tx.Error != nil {
		return RecordResult{}, errors.NewErr("ErrorEventSaveFailed", tx.Error).
			SetMessage("The mistakes of this check could not be saved to the statistics.")
	}
	res.Inserted = int(tx.RowsAffected)
	res.Duplicates = len(events) - res.Inserted
	return res, nil
}

// NotAnError deletes the recorded mistake with this fingerprint in the source
// and remembers, for the rest of the session, not to record it again. It is
// not an error if nothing was recorded.
func (st *Stats) NotAnError(ctx context.Context, sourceKey, fingerprint string) error {
	st.mu.Lock()
	st.ignored[ignoreKey{sourceKey, fingerprint}] = struct{}{}
	st.mu.Unlock()
	err := st.store.db.WithContext(ctx).
		Where("source_key = ? AND fingerprint = ?", sourceKey, fingerprint).
		Delete(&ErrorEvent{}).Error
	if err != nil {
		return errors.NewErr("StatsResetFailed", err).
			SetMessage("The mistake could not be removed from the statistics.")
	}
	return nil
}

// MoveSource gives the mistakes of one source to another, for Save As (section
// 5.7): the text keeps one identity, so nothing is copied or counted twice. A
// mistake that the new source already has is dropped from the old one.
func (st *Stats) MoveSource(ctx context.Context, from, to string) error {
	if from == to {
		return nil
	}
	err := st.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		already := tx.Model(&ErrorEvent{}).Select("fingerprint").Where("source_key = ?", to)
		if err := tx.Where("source_key = ?", from).
			Where("fingerprint IN (?)", already).
			Delete(&ErrorEvent{}).Error; err != nil {
			return err
		}
		return tx.Model(&ErrorEvent{}).Where("source_key = ?", from).Update("source_key", to).Error
	})
	if err != nil {
		return errors.NewErr("ErrorEventSaveFailed", err).
			SetMessage("The statistics could not follow the text to its new location.")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for k := range st.ignored {
		if k.source == from {
			delete(st.ignored, k)
			st.ignored[ignoreKey{to, k.fingerprint}] = struct{}{}
		}
	}
	return nil
}

func (st *Stats) isIgnored(source, fingerprint string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	_, ok := st.ignored[ignoreKey{source, fingerprint}]
	return ok
}

// ResetMistakes deletes every recorded mistake. The session's ignore set stays.
func (st *Stats) ResetMistakes(ctx context.Context) error {
	err := st.store.db.WithContext(ctx).Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&ErrorEvent{}).Error
	if err != nil {
		return errors.NewErr("StatsResetFailed", err).
			SetMessage("The statistics could not be reset.")
	}
	return nil
}

// TypeStat is the row of the Statistics table for one error type.
type TypeStat struct {
	ErrorType string
	Category  string
	Total     int
	Last30    int
	Last7     int
	LastSeen  time.Time
}

// Count returns the count for a period.
func (t TypeStat) Count(p Period) int {
	switch p {
	case Last30Days:
		return t.Last30
	case Last7Days:
		return t.Last7
	default:
		return t.Total
	}
}

type countRow struct {
	ErrorType string
	Category  string
	N         int
	LastSeen  string
}

// TypeStats returns one row per error type with at least one recorded
// mistake, most frequent first (ties by error type).
func (st *Stats) TypeStats(ctx context.Context) ([]TypeStat, error) {
	now := st.clock()
	byType := map[string]*TypeStat{}
	var order []string
	for _, p := range []Period{AllTime, Last30Days, Last7Days} {
		rows, err := st.groupedCounts(ctx, p, now)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			ts, ok := byType[r.ErrorType]
			if !ok {
				ts = &TypeStat{ErrorType: r.ErrorType, Category: r.Category}
				byType[r.ErrorType] = ts
				order = append(order, r.ErrorType)
			}
			switch p {
			case AllTime:
				ts.Total = r.N
				ts.LastSeen = parseTime(r.LastSeen)
			case Last30Days:
				ts.Last30 = r.N
			case Last7Days:
				ts.Last7 = r.N
			}
		}
	}
	out := make([]TypeStat, 0, len(order))
	for _, id := range order {
		out = append(out, *byType[id])
	}
	sortTypeStats(out)
	return out, nil
}

func (st *Stats) groupedCounts(ctx context.Context, p Period, now time.Time) ([]countRow, error) {
	q := st.store.db.WithContext(ctx).Model(&ErrorEvent{}).
		Select("error_type", "category", "COUNT(*) AS n", "MAX(created_at) AS last_seen").
		Group("error_type").Group("category")
	if since, ok := p.since(now); ok {
		q = q.Where("created_at >= ?", since)
	}
	var rows []countRow
	if err := q.Scan(&rows).Error; err != nil {
		return nil, errors.NewErr("StatsQueryFailed", err)
	}
	return rows, nil
}

// TypeCount is how often one error type was made, for the panel header.
type TypeCount struct {
	Total  int
	Last7  int
	Last30 int
}

// CountForType returns the counts of one error type.
func (st *Stats) CountForType(ctx context.Context, errorType string) (TypeCount, error) {
	now := st.clock()
	var tc TypeCount
	for _, w := range []struct {
		p   Period
		dst *int
	}{{AllTime, &tc.Total}, {Last30Days, &tc.Last30}, {Last7Days, &tc.Last7}} {
		q := st.store.db.WithContext(ctx).Model(&ErrorEvent{}).Where("error_type = ?", errorType)
		if since, ok := w.p.since(now); ok {
			q = q.Where("created_at >= ?", since)
		}
		var n int64
		if err := q.Count(&n).Error; err != nil {
			return TypeCount{}, errors.NewErr("StatsQueryFailed", err).SetMetadata("error_type", errorType)
		}
		*w.dst = int(n)
	}
	return tc, nil
}

// Example is one thing the user actually wrote wrong.
type Example struct {
	Snippet   string
	CreatedAt time.Time
}

// RecentExamples returns up to limit snippets of an error type, newest first.
func (st *Stats) RecentExamples(ctx context.Context, errorType string, limit int) ([]Example, error) {
	var events []ErrorEvent
	err := st.store.db.WithContext(ctx).
		Where("error_type = ?", errorType).
		Order("created_at desc").Order("id desc").
		Limit(limit).Find(&events).Error
	if err != nil {
		return nil, errors.NewErr("StatsQueryFailed", err).SetMetadata("error_type", errorType)
	}
	out := make([]Example, len(events))
	for i, e := range events {
		out[i] = Example{Snippet: e.Snippet, CreatedAt: e.CreatedAt}
	}
	return out, nil
}
