package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// PaymentsCollectedRepository is what the service needs from storage. *repository.PaymentsCollectedRepository
// satisfies it.
type PaymentsCollectedRepository interface {
	// Page returns one page and, when withSummary is set, the totals of the whole filtered set, both read from
	// one snapshot so the summary always describes the rows' set.
	Page(ctx context.Context, q model.PaymentCollectedQuery, withSummary bool) (model.PaymentCollectedPage, error)
	Staff(ctx context.Context) ([]model.StaffOption, error)
}

// PaymentsCollectedService implements the Payments Collected report.
type PaymentsCollectedService struct {
	repo PaymentsCollectedRepository
	now  Clock
}

// NewPaymentsCollectedService returns a service over repo. A nil clock reads the current time in UTC.
func NewPaymentsCollectedService(repo PaymentsCollectedRepository, clock Clock) *PaymentsCollectedService {
	if clock == nil {
		clock = utcNow
	}
	return &PaymentsCollectedService{repo: repo, now: clock}
}

// ListPaymentsCollectedResult is one page of the report.
type ListPaymentsCollectedResult struct {
	Items      []model.PaymentCollectedRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary totals the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.PaymentCollectedSummary
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *PaymentsCollectedService) List(ctx context.Context, p ListPaymentsCollectedParams) (*ListPaymentsCollectedResult, error) {
	req, err := parsePaymentsCollectedParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	// The summary is wanted on the first page only; later pages keep the one the client already has.
	page, err := s.repo.Page(ctx, q, q.After == nil)
	if err != nil {
		return nil, fmt.Errorf("list payments collected: %w", err)
	}
	rows := page.Rows

	res := &ListPaymentsCollectedResult{Limit: q.Limit, From: req.from, To: req.to, Summary: page.Summary}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodePaymentCollectedCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.PaymentCollectedRow{}
	}
	res.Items = rows
	return res, nil
}

// PaymentsCollectedFilterOptions are the choices offered by the report's filter dropdowns that come from data.
type PaymentsCollectedFilterOptions struct {
	Staff []model.StaffOption
}

// FilterOptions returns the staff members that can be filtered on.
func (s *PaymentsCollectedService) FilterOptions(ctx context.Context) (*PaymentsCollectedFilterOptions, error) {
	staff, err := s.repo.Staff(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	return &PaymentsCollectedFilterOptions{Staff: staff}, nil
}
