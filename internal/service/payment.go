package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// PaymentRepository is what the service needs from storage. *repository.PaymentRepository satisfies it.
type PaymentRepository interface {
	List(ctx context.Context, q model.PaymentQuery) ([]model.PaymentRow, error)
	Summary(ctx context.Context, f model.PaymentFilter) (model.PaymentSummary, error)
}

// PaymentService implements the Payments report.
type PaymentService struct {
	repo PaymentRepository
	now  Clock
}

// NewPaymentService returns a service over repo. A nil clock uses time.Now.
func NewPaymentService(repo PaymentRepository, clock Clock) *PaymentService {
	if clock == nil {
		clock = time.Now
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

	rows, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}

	res := &ListPaymentsResult{Limit: q.Limit, From: req.from, To: req.to}
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

	if q.After == nil {
		summary, err := s.repo.Summary(ctx, q.Filter)
		if err != nil {
			return nil, fmt.Errorf("summarize payments: %w", err)
		}
		res.Summary = &summary
	}
	return res, nil
}
