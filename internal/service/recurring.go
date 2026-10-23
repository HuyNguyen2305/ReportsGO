package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// RecurringRepository is what the service needs from storage. *repository.RecurringRepository satisfies it.
type RecurringRepository interface {
	List(ctx context.Context, q model.RecurringQuery) ([]model.RecurringRow, error)
	Summary(ctx context.Context, f model.RecurringFilter) (model.RecurringSummary, error)
}

// RecurringService implements the Recurring Invoices report. It needs no clock: the report has no default date
// range that depends on today's date.
type RecurringService struct {
	repo RecurringRepository
}

// NewRecurringService returns a service over repo.
func NewRecurringService(repo RecurringRepository) *RecurringService {
	return &RecurringService{repo: repo}
}

// ListRecurringResult is one page of the report.
type ListRecurringResult struct {
	Items      []model.RecurringRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary counts and totals the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.RecurringSummary
	// From and To are the effective start-date range; zero when that side is open (both are zero by default).
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *RecurringService) List(ctx context.Context, p ListRecurringParams) (*ListRecurringResult, error) {
	req, err := parseRecurringParams(p)
	if err != nil {
		return nil, err
	}
	q := req.query

	rows, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list recurring invoices: %w", err)
	}

	res := &ListRecurringResult{Limit: q.Limit, From: req.from, To: req.to}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeRecurringCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.RecurringRow{}
	}
	res.Items = rows

	if q.After == nil {
		summary, err := s.repo.Summary(ctx, q.Filter)
		if err != nil {
			return nil, fmt.Errorf("summarize recurring invoices: %w", err)
		}
		res.Summary = &summary
	}
	return res, nil
}
