package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// ServiceLookupRepository is what the service needs from storage. *repository.ServiceLookupRepository satisfies it.
type ServiceLookupRepository interface {
	// Page returns one page and, when withSummary is set, the totals of the whole filtered set, both read from
	// one snapshot so the summary always describes the rows' set.
	Page(ctx context.Context, q model.ServiceLookupQuery, withSummary bool) (model.ServiceLookupPage, error)
	ServiceTypes(ctx context.Context) ([]model.ServiceType, error)
	Staff(ctx context.Context) ([]model.StaffOption, error)
	Tags(ctx context.Context) ([]model.JobTag, error)
}

// ServiceLookupService implements the Service Lookup report.
type ServiceLookupService struct {
	repo ServiceLookupRepository
	now  Clock
}

// NewServiceLookupService returns a service over repo. A nil clock reads the current time in UTC.
func NewServiceLookupService(repo ServiceLookupRepository, clock Clock) *ServiceLookupService {
	if clock == nil {
		clock = utcNow
	}
	return &ServiceLookupService{repo: repo, now: clock}
}

// ListServiceLookupResult is one page of the report.
type ListServiceLookupResult struct {
	Items      []model.ServiceLookupRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary totals the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.ServiceLookupSummary
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *ServiceLookupService) List(ctx context.Context, p ListServiceLookupParams) (*ListServiceLookupResult, error) {
	req, err := parseServiceLookupParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	// The summary is wanted on the first page only; later pages keep the one the client already has.
	page, err := s.repo.Page(ctx, q, q.After == nil)
	if err != nil {
		return nil, fmt.Errorf("list service lookup: %w", err)
	}
	rows := page.Rows

	res := &ListServiceLookupResult{Limit: q.Limit, From: req.from, To: req.to, Summary: page.Summary}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeServiceLookupCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.ServiceLookupRow{}
	}
	res.Items = rows
	return res, nil
}

// ServiceLookupFilterOptions are the choices offered by the report's filter dropdowns that come from data.
type ServiceLookupFilterOptions struct {
	ServiceTypes []model.ServiceType
	Staff        []model.StaffOption
	Tags         []model.JobTag
}

// FilterOptions returns the service types, staff members and tags that can be filtered on.
func (s *ServiceLookupService) FilterOptions(ctx context.Context) (*ServiceLookupFilterOptions, error) {
	types, err := s.repo.ServiceTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	staff, err := s.repo.Staff(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	tags, err := s.repo.Tags(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	return &ServiceLookupFilterOptions{ServiceTypes: types, Staff: staff, Tags: tags}, nil
}
