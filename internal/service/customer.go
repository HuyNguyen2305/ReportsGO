package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// CustomerRepository is what the service needs from storage. *repository.CustomerRepository satisfies it.
type CustomerRepository interface {
	List(ctx context.Context, q model.CustomerQuery) ([]model.CustomerRow, error)
	Summary(ctx context.Context, f model.CustomerFilter) (model.CustomerSummary, error)
	LeadSources(ctx context.Context) ([]model.LeadSource, error)
}

// CustomerService implements the New Customers report.
type CustomerService struct {
	repo CustomerRepository
	now  Clock
}

// NewCustomerService returns a service over repo. A nil clock reads the current time in UTC.
func NewCustomerService(repo CustomerRepository, clock Clock) *CustomerService {
	if clock == nil {
		clock = utcNow
	}
	return &CustomerService{repo: repo, now: clock}
}

// ListCustomersResult is one page of the report.
type ListCustomersResult struct {
	Items      []model.CustomerRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary counts the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.CustomerSummary
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *CustomerService) List(ctx context.Context, p ListCustomersParams) (*ListCustomersResult, error) {
	req, err := parseCustomerParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	rows, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list customers: %w", err)
	}

	res := &ListCustomersResult{Limit: q.Limit, From: req.from, To: req.to}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeCustomerCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.CustomerRow{}
	}
	res.Items = rows

	if q.After == nil {
		summary, err := s.repo.Summary(ctx, q.Filter)
		if err != nil {
			return nil, fmt.Errorf("summarize customers: %w", err)
		}
		res.Summary = &summary
	}
	return res, nil
}

// CustomerFilterOptions are the choices offered by the report's filter dropdowns.
type CustomerFilterOptions struct {
	LeadSources []model.LeadSource
}

// FilterOptions returns the lead sources that can be filtered on.
func (s *CustomerService) FilterOptions(ctx context.Context) (*CustomerFilterOptions, error) {
	sources, err := s.repo.LeadSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	if sources == nil {
		sources = []model.LeadSource{}
	}
	return &CustomerFilterOptions{LeadSources: sources}, nil
}
