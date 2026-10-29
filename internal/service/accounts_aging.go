package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// AccountsAgingRepository is what the service needs from storage. *repository.AccountsAgingRepository satisfies it.
type AccountsAgingRepository interface {
	// Page returns one page and, when withSummary is set, the totals of the whole filtered set, both read from
	// one snapshot so the summary always describes the rows' set.
	Page(ctx context.Context, q model.AgingQuery, withSummary bool) (model.AgingPage, error)
	ServiceTypes(ctx context.Context) ([]model.ServiceType, error)
}

// AccountsAgingService implements the Accounts Aging report.
type AccountsAgingService struct {
	repo AccountsAgingRepository
	now  Clock
}

// NewAccountsAgingService returns a service over repo. A nil clock reads the current time in UTC.
func NewAccountsAgingService(repo AccountsAgingRepository, clock Clock) *AccountsAgingService {
	if clock == nil {
		clock = utcNow
	}
	return &AccountsAgingService{repo: repo, now: clock}
}

// ListAgingResult is one page of the report.
type ListAgingResult struct {
	Items      []model.AgingRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary totals the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.AgingSummary
	// AsOf is the day the balances are read at, including a defaulted today.
	AsOf time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *AccountsAgingService) List(ctx context.Context, p ListAgingParams) (*ListAgingResult, error) {
	req, err := parseAgingParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	// The summary is wanted on the first page only; later pages keep the one the client already has.
	page, err := s.repo.Page(ctx, q, q.After == nil)
	if err != nil {
		return nil, fmt.Errorf("list accounts aging: %w", err)
	}
	rows := page.Rows

	res := &ListAgingResult{Limit: q.Limit, AsOf: req.asOf, Summary: page.Summary}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeAgingCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.AgingRow{}
	}
	res.Items = rows
	return res, nil
}

// AgingFilterOptions are the choices offered by the report's filter dropdowns that come from data.
type AgingFilterOptions struct {
	ServiceTypes []model.ServiceType
}

// FilterOptions returns the service types that can be filtered on.
func (s *AccountsAgingService) FilterOptions(ctx context.Context) (*AgingFilterOptions, error) {
	types, err := s.repo.ServiceTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	return &AgingFilterOptions{ServiceTypes: types}, nil
}
