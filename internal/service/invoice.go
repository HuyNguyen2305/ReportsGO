package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// InvoiceRepository is what the service needs from storage. *repository.InvoiceRepository satisfies it.
type InvoiceRepository interface {
	List(ctx context.Context, q model.InvoiceQuery) ([]model.InvoiceRow, error)
	Summary(ctx context.Context, f model.InvoiceFilter) (model.InvoiceSummary, error)
	ServiceTypes(ctx context.Context) ([]model.ServiceType, error)
	Tags(ctx context.Context) ([]model.InvoiceTag, error)
	PaymentTerms(ctx context.Context) ([]model.PaymentTerm, error)
}

// Clock returns the current time; it is injected so the default date range is testable.
type Clock func() time.Time

// InvoiceService implements the Invoices report.
type InvoiceService struct {
	repo InvoiceRepository
	now  Clock
}

// NewInvoiceService returns a service over repo. A nil clock uses time.Now.
func NewInvoiceService(repo InvoiceRepository, clock Clock) *InvoiceService {
	if clock == nil {
		clock = time.Now
	}
	return &InvoiceService{repo: repo, now: clock}
}

// ListInvoicesResult is one page of the report.
type ListInvoicesResult struct {
	Items      []model.InvoiceRow
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary totals the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.InvoiceSummary
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *InvoiceService) List(ctx context.Context, p ListInvoicesParams) (*ListInvoicesResult, error) {
	req, err := parseInvoiceParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	rows, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}

	res := &ListInvoicesResult{From: req.from, To: req.to}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeInvoiceCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.InvoiceRow{}
	}
	res.Items = rows

	if q.After == nil {
		summary, err := s.repo.Summary(ctx, q.Filter)
		if err != nil {
			return nil, fmt.Errorf("summarize invoices: %w", err)
		}
		res.Summary = &summary
	}
	return res, nil
}

// InvoiceFilterOptions are the choices offered by the report's filter dropdowns.
type InvoiceFilterOptions struct {
	ServiceTypes []model.ServiceType
	Tags         []model.InvoiceTag
	PaymentTerms []model.PaymentTerm
}

// FilterOptions returns the service types, tags and payment terms that can be filtered on.
func (s *InvoiceService) FilterOptions(ctx context.Context) (*InvoiceFilterOptions, error) {
	serviceTypes, err := s.repo.ServiceTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	tags, err := s.repo.Tags(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	terms, err := s.repo.PaymentTerms(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	return &InvoiceFilterOptions{ServiceTypes: serviceTypes, Tags: tags, PaymentTerms: terms}, nil
}
