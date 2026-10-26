package service

import (
	"context"
	"fmt"
	"time"

	"reportsgo/internal/model"
)

// JobRepository is what the service needs from storage. *repository.JobRepository satisfies it.
type JobRepository interface {
	List(ctx context.Context, q model.JobQuery) ([]model.JobRow, error)
	Summary(ctx context.Context, f model.JobFilter) (model.JobSummary, error)
	ServiceTypes(ctx context.Context) ([]model.ServiceType, error)
	LeadSources(ctx context.Context) ([]model.LeadSource, error)
}

// JobService implements the New Jobs report.
type JobService struct {
	repo JobRepository
	now  Clock
}

// NewJobService returns a service over repo. A nil clock reads the current time in UTC.
func NewJobService(repo JobRepository, clock Clock) *JobService {
	if clock == nil {
		clock = utcNow
	}
	return &JobService{repo: repo, now: clock}
}

// ListJobsResult is one page of the report.
type ListJobsResult struct {
	Items      []model.JobRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary counts and totals the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.JobSummary
	// From and To are the effective date range, including a defaulted month; zero when that side is open.
	From, To time.Time
}

// List validates p and returns one page of the report. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are.
func (s *JobService) List(ctx context.Context, p ListJobsParams) (*ListJobsResult, error) {
	req, err := parseJobParams(p, s.now())
	if err != nil {
		return nil, err
	}
	q := req.query

	rows, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}

	res := &ListJobsResult{Limit: q.Limit, From: req.from, To: req.to}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeJobCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.JobRow{}
	}
	res.Items = rows

	if q.After == nil {
		summary, err := s.repo.Summary(ctx, q.Filter)
		if err != nil {
			return nil, fmt.Errorf("summarize jobs: %w", err)
		}
		res.Summary = &summary
	}
	return res, nil
}

// JobFilterOptions are the choices offered by the report's filter dropdowns.
type JobFilterOptions struct {
	ServiceTypes []model.ServiceType
	LeadSources  []model.LeadSource
}

// FilterOptions returns the service types and lead sources that can be filtered on.
func (s *JobService) FilterOptions(ctx context.Context) (*JobFilterOptions, error) {
	types, err := s.repo.ServiceTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	sources, err := s.repo.LeadSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("load filter options: %w", err)
	}
	if types == nil {
		types = []model.ServiceType{}
	}
	if sources == nil {
		sources = []model.LeadSource{}
	}
	return &JobFilterOptions{ServiceTypes: types, LeadSources: sources}, nil
}
