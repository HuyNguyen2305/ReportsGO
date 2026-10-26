package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// EstimateRepository is what the service needs from storage. *repository.EstimateRepository satisfies it.
type EstimateRepository interface {
	// Page returns one page and, when withSummary is set, the totals of the whole filtered set, both read from
	// one snapshot so the summary always describes the rows' set.
	Page(ctx context.Context, q model.EstimateQuery, withSummary bool) (model.EstimatePage, error)
}

// EstimateService implements the Estimates report.
type EstimateService struct {
	repo EstimateRepository
	now  Clock
}

// NewEstimateService returns a service over repo. A nil clock reads the current time in UTC.
func NewEstimateService(repo EstimateRepository, clock Clock) *EstimateService {
	if clock == nil {
		clock = utcNow
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

	// The summary is wanted on the first page only; later pages keep the one the client already has.
	page, err := s.repo.Page(ctx, q, q.After == nil)
	if err != nil {
		return nil, fmt.Errorf("list estimates: %w", err)
	}
	rows := page.Rows

	res := &ListEstimatesResult{Limit: q.Limit, From: req.from, To: req.to, Summary: page.Summary}
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
	return res, nil
}
