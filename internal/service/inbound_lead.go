package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// InboundLeadRepository is what the service needs from storage. *repository.InboundLeadRepository satisfies it.
type InboundLeadRepository interface {
	// List returns up to q.Limit+1 rows, so the service can tell whether another page follows.
	List(ctx context.Context, q model.InboundLeadQuery) ([]model.InboundLeadRow, error)
	LeadSources(ctx context.Context) ([]model.LeadSource, error)
}

// InboundLeadService implements the Inbound Leads report.
type InboundLeadService struct {
	repo InboundLeadRepository
	now  Clock
}

// NewInboundLeadService returns a service over repo. A nil clock reads the current time in UTC.
func NewInboundLeadService(repo InboundLeadRepository, clock Clock) *InboundLeadService {
	if clock == nil {
		clock = utcNow
	}
	return &InboundLeadService{repo: repo, now: clock}
}

// ListInboundLeadsResult is one page of the report.
type ListInboundLeadsResult struct {
	Items      []model.InboundLeadRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *InboundLeadService) List(ctx context.Context, p ListInboundLeadsParams) (*ListInboundLeadsResult, error) {
	req, err := parseInboundLeadParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	rows, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list inbound leads: %w", err)
	}

	res := &ListInboundLeadsResult{Limit: q.Limit, From: req.from, To: req.to}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeInboundLeadCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.InboundLeadRow{}
	}
	res.Items = rows
	return res, nil
}

// InboundLeadFilterOptions are the choices offered by the report's filter dropdowns. The name formats are a fixed
// set, so only the sources come from storage.
type InboundLeadFilterOptions struct {
	Sources []model.LeadSource
}

// FilterOptions returns the lead sources that can be filtered on.
func (s *InboundLeadService) FilterOptions(ctx context.Context) (*InboundLeadFilterOptions, error) {
	sources, err := s.repo.LeadSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	if sources == nil {
		sources = []model.LeadSource{}
	}
	return &InboundLeadFilterOptions{Sources: sources}, nil
}
