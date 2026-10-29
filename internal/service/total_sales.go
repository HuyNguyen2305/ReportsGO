package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// TotalSalesRepository is what the service needs from storage. *repository.TotalSalesRepository satisfies it.
type TotalSalesRepository interface {
	// Page returns one page and, when withSummary is set, the totals of the whole filtered set, both read from
	// one snapshot so the summary always describes the rows' set.
	Page(ctx context.Context, q model.TotalSaleQuery, withSummary bool) (model.TotalSalePage, error)
}

// TotalSalesService implements the Total Sales report.
type TotalSalesService struct {
	repo TotalSalesRepository
	now  Clock
}

// NewTotalSalesService returns a service over repo. A nil clock reads the current time in UTC.
func NewTotalSalesService(repo TotalSalesRepository, clock Clock) *TotalSalesService {
	if clock == nil {
		clock = utcNow
	}
	return &TotalSalesService{repo: repo, now: clock}
}

// ListTotalSalesResult is one page of the report.
type ListTotalSalesResult struct {
	Items      []model.TotalSaleRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary totals the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.TotalSaleSummary
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *TotalSalesService) List(ctx context.Context, p ListTotalSalesParams) (*ListTotalSalesResult, error) {
	req, err := parseTotalSaleParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	// The summary is wanted on the first page only; later pages keep the one the client already has.
	page, err := s.repo.Page(ctx, q, q.After == nil)
	if err != nil {
		return nil, fmt.Errorf("list total sales: %w", err)
	}
	rows := page.Rows

	res := &ListTotalSalesResult{Limit: q.Limit, From: req.from, To: req.to, Summary: page.Summary}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeTotalSaleCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.TotalSaleRow{}
	}
	res.Items = rows
	return res, nil
}
