package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// EstimateRepository is what the service needs from storage. *repository.EstimateRepository satisfies it.
type EstimateRepository interface {
	List(ctx context.Context, q model.EstimateQuery) ([]model.EstimateRow, error)
	Summary(ctx context.Context, f model.EstimateFilter) (model.EstimateSummary, error)
}

// EstimateService implements the Estimates report.
type EstimateService struct {
	repo EstimateRepository
	now  Clock
}

// NewEstimateService returns a service over repo. A nil clock uses time.Now.
func NewEstimateService(repo EstimateRepository, clock Clock) *EstimateService {
	if clock == nil {
		clock = time.Now
	}
	return &EstimateService{repo: repo, now: clock}
}

// ListEstimatesResult is one page of the report.
type ListEstimatesResult struct {
	Items      []model.EstimateRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary totals the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.EstimateSummary
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *EstimateService) List(ctx context.Context, p ListEstimatesParams) (*ListEstimatesResult, error) {
	req, err := parseEstimateParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	rows, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list estimates: %w", err)
	}

	res := &ListEstimatesResult{Limit: q.Limit, From: req.from, To: req.to}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeEstimateCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.EstimateRow{}
	}
	res.Items = rows

	if q.After == nil {
		summary, err := s.repo.Summary(ctx, q.Filter)
		if err != nil {
			return nil, fmt.Errorf("summarize estimates: %w", err)
		}
		res.Summary = &summary
	}
	return res, nil
}
