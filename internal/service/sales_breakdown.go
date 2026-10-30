package service

import (
	"context"
	"fmt"

	"reportsgo/internal/model"
)

// SalesBreakdownRepository is what the service needs from storage. *repository.SalesBreakdownRepository satisfies it.
type SalesBreakdownRepository interface {
	// Page returns one page and, when withSummary is set, the summary and the unassigned line, all read from one
	// snapshot.
	Page(ctx context.Context, q model.SalesBreakdownQuery, withSummary bool) (model.SalesBreakdownPage, error)
	GroupOptions(ctx context.Context) (model.SalesBreakdownGroupOptions, error)
}

// SalesBreakdownService implements the Revenue and Sales Forecast reports.
type SalesBreakdownService struct {
	repo SalesBreakdownRepository
	now  Clock
}

// NewSalesBreakdownService returns a service over repo. A nil clock reads the current time in UTC.
func NewSalesBreakdownService(repo SalesBreakdownRepository, clock Clock) *SalesBreakdownService {
	if clock == nil {
		clock = utcNow
	}
	return &SalesBreakdownService{repo: repo, now: clock}
}

// ListSalesBreakdownResult is one page of the grid.
type ListSalesBreakdownResult struct {
	Items      []model.SalesBreakdownRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Unassigned is the amount that belongs to no group. It is set on the first page only, and only when it is not
	// zero and no group or search filter is set.
	Unassigned *model.SalesBreakdownAmounts
	// Summary is the true total of the report. It is set on the first page only (no cursor sent).
	Summary *model.SalesBreakdownSummary
	Type    model.SalesBreakdownType
	By      model.SalesBreakdownBy
	Year    int
}

// List validates p and returns one page of the grid. Invalid input is reported as model.InvalidInput, with a detail
// per offending field; storage errors are wrapped and returned as they are.
func (s *SalesBreakdownService) List(ctx context.Context, p ListSalesBreakdownParams) (*ListSalesBreakdownResult, error) {
	q, err := parseSalesBreakdownParams(p, s.now())
	if err != nil {
		return nil, err
	}

	// The summary is wanted on the first page only; later pages keep the one the client already has.
	page, err := s.repo.Page(ctx, q, q.After == nil)
	if err != nil {
		return nil, fmt.Errorf("list sales breakdown: %w", err)
	}
	rows := page.Rows

	res := &ListSalesBreakdownResult{
		Limit: q.Limit, Unassigned: page.Unassigned, Summary: page.Summary,
		Type: q.Filter.Type, By: q.Filter.By, Year: q.Filter.Year,
	}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeSalesBreakdownCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.SalesBreakdownRow{}
	}
	res.Items = rows
	return res, nil
}

// FilterOptions returns the choices of the group filter for each grouping but clients.
func (s *SalesBreakdownService) FilterOptions(ctx context.Context) (*model.SalesBreakdownGroupOptions, error) {
	opts, err := s.repo.GroupOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	return &opts, nil
}
