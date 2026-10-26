package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// PaymentRepository is what the service needs from storage. *repository.PaymentRepository satisfies it.
type PaymentRepository interface {
	// Page returns one page and, when withSummary is set, the count and total of the whole filtered set, both
	// read from one snapshot so the summary always describes the rows' set.
	Page(ctx context.Context, q model.PaymentQuery, withSummary bool) (model.PaymentPage, error)
}

// PaymentService implements the Payments report.
type PaymentService struct {
	repo PaymentRepository
	now  Clock
}

// NewPaymentService returns a service over repo. A nil clock reads the current time in UTC.
func NewPaymentService(repo PaymentRepository, clock Clock) *PaymentService {
	if clock == nil {
		clock = utcNow
	}
	return &PaymentService{repo: repo, now: clock}
}

// ListPaymentsResult is one page of the report.
type ListPaymentsResult struct {
	Items      []model.PaymentRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary counts and totals the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.PaymentSummary
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *PaymentService) List(ctx context.Context, p ListPaymentsParams) (*ListPaymentsResult, error) {
	req, err := parsePaymentParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	// The summary is wanted on the first page only; later pages keep the one the client already has.
	page, err := s.repo.Page(ctx, q, q.After == nil)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	rows := page.Rows

	res := &ListPaymentsResult{Limit: q.Limit, From: req.from, To: req.to, Summary: page.Summary}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodePaymentCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.PaymentRow{}
	}
	res.Items = rows
	return res, nil
}
