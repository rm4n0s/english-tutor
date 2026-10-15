package store

import (
	"context"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"
	"gorm.io/gorm"

	"github.com/rm4n0s/english-tutor/internal/analysis"
)

// RecordUsage saves the token usage of one request. It makes *Stats an
// analysis.UsageRecorder.
func (st *Stats) RecordUsage(ctx context.Context, rec analysis.UsageRecord) error {
	row := LLMUsage{
		LLMName:      rec.LLM,
		Kind:         string(rec.Kind),
		InputTokens:  rec.Usage.InputTokens,
		OutputTokens: rec.Usage.OutputTokens,
		Estimated:    rec.Usage.Estimated,
		CreatedAt:    st.clock(),
	}
	if err := st.store.db.WithContext(ctx).Create(&row).Error; err != nil {
		return errors.NewErr("UsageSaveFailed", err).
			SetMessage("The token usage of this request could not be saved.")
	}
	return nil
}

// UsageTotal is the token usage of one LLM in one period.
type UsageTotal struct {
	LLMName      string
	InputTokens  int
	OutputTokens int
	// Estimated is true if any request in the sum had estimated counts.
	Estimated bool
}

type usageRow struct {
	LLMName   string
	Input     int
	Output    int
	Estimated int
}

// UsageTotals sums the recorded requests per LLM in the period, ordered by
// LLM name.
func (st *Stats) UsageTotals(ctx context.Context, p Period) ([]UsageTotal, error) {
	q := st.store.db.WithContext(ctx).Model(&LLMUsage{}).
		Select("llm_name", "SUM(input_tokens) AS input", "SUM(output_tokens) AS output", "MAX(estimated) AS estimated").
		Group("llm_name").Order("llm_name")
	if since, ok := p.since(st.clock()); ok {
		q = q.Where("created_at >= ?", since)
	}
	var rows []usageRow
	if err := q.Scan(&rows).Error; err != nil {
		return nil, errors.NewErr("StatsQueryFailed", err)
	}
	out := make([]UsageTotal, len(rows))
	for i, r := range rows {
		out[i] = UsageTotal{LLMName: r.LLMName, InputTokens: r.Input, OutputTokens: r.Output, Estimated: r.Estimated != 0}
	}
	slices.SortStableFunc(out, func(a, b UsageTotal) int { return strings.Compare(a.LLMName, b.LLMName) })
	return out, nil
}

// ResetUsage deletes every recorded request.
func (st *Stats) ResetUsage(ctx context.Context) error {
	if err := st.store.db.WithContext(ctx).Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&LLMUsage{}).Error; err != nil {
		return errors.NewErr("StatsResetFailed", err).
			SetMessage("The token usage could not be reset.")
	}
	return nil
}

var _ analysis.UsageRecorder = (*Stats)(nil)
