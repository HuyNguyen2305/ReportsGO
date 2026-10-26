package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// InvoiceRepository is what the service needs from storage. *repository.InvoiceRepository satisfies it.
type InvoiceRepository interface {
	// Page returns one page and, when withSummary is set, the totals of the whole filtered set, both read from
	// one snapshot so the summary always describes the rows' set.
	Page(ctx context.Context, q model.InvoiceQuery, withSummary bool) (model.InvoicePage, error)
	ServiceTypes(ctx context.Context) ([]model.ServiceType, error)
	Tags(ctx context.Context) ([]model.InvoiceTag, error)
	PaymentTerms(ctx context.Context) ([]model.PaymentTerm, error)
}

// Clock returns the current time; it is injected so the default date range is testable. The zone of the
// returned time decides which calendar month the default range covers (see validator.dateRange).
type Clock func() time.Time

// utcNow is the default Clock: the current time in UTC. Without it a nil clock would read time.Now in the host's
// local zone, making the default month depend on the machine instead of on configuration.
func utcNow() time.Time { return time.Now().UTC() }

// InvoiceService implements the Invoices report.
type InvoiceService struct {
	repo InvoiceRepository
	now  Clock
}

// NewInvoiceService returns a service over repo. A nil clock reads the current time in UTC.
func NewInvoiceService(repo InvoiceRepository, clock Clock) *InvoiceService {
	if clock == nil {
		clock = utcNow
	}
	return &InvoiceService{repo: repo, now: clock}
}

// ListInvoicesResult is one page of the report.
type ListInvoicesResult struct {
	Items      []model.InvoiceRow
	Limit      int    // the effective page size
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

	// The summary is wanted on the first page only; later pages keep the one the client already has.
	page, err := s.repo.Page(ctx, q, q.After == nil)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	rows := page.Rows

	res := &ListInvoicesResult{Limit: q.Limit, From: req.from, To: req.to, Summary: page.Summary}
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
