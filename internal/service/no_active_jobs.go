package service

import (
	"context"
	"fmt"

	"reportsgo/internal/model"
)

// NoActiveJobsRepository is what the service needs from storage. *repository.NoActiveJobsRepository satisfies it.
type NoActiveJobsRepository interface {
	// Page returns one page and, when withSummary is set, the count of the whole filtered set, both read from
	// one snapshot so the summary always describes the rows' set.
	Page(ctx context.Context, q model.NoActiveJobsQuery, withSummary bool) (model.NoActiveJobsPage, error)
}

// NoActiveJobsService implements the Customers and Locations w/o Active Jobs reports.
type NoActiveJobsService struct {
	repo NoActiveJobsRepository
	now  Clock
}

// NewNoActiveJobsService returns a service over repo. A nil clock reads the current time in UTC.
func NewNoActiveJobsService(repo NoActiveJobsRepository, clock Clock) *NoActiveJobsService {
	if clock == nil {
		clock = utcNow
	}
	return &NoActiveJobsService{repo: repo, now: clock}
}

// ListNoActiveJobsResult is one page of the report.
type ListNoActiveJobsResult struct {
	Items      []model.NoActiveJobsRow
	Limit      int    // the effective page size
	NextCursor string // empty when there is no next page
	HasMore    bool
	// Summary counts the whole filtered set. It is set on the first page only (no cursor sent).
	Summary *model.NoActiveJobsSummary
}

// List validates p and returns one page of the report of grain. Invalid input is reported as model.InvalidInput,
// with a detail per offending field; storage errors are wrapped and returned as they are. The time of each last
// service is returned in the clock's time zone, so its calendar day is the business day.
func (s *NoActiveJobsService) List(ctx context.Context, grain model.NoActiveJobsGrain, p ListNoActiveJobsParams) (*ListNoActiveJobsResult, error) {
	q, err := parseNoActiveJobsParams(grain, p)
	if err != nil {
		return nil, err
	}

	// The summary is wanted on the first page only; later pages keep the one the client already has.
	page, err := s.repo.Page(ctx, q, q.After == nil)
	if err != nil {
		return nil, fmt.Errorf("list %s without active jobs: %w", grain, err)
	}
	rows := page.Rows

	res := &ListNoActiveJobsResult{Limit: q.Limit, Summary: page.Summary}
	// The repository returns one row beyond the page size so we can tell whether another page exists.
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		res.HasMore = true
		res.NextCursor = EncodeNoActiveJobsCursor(q.Sort, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []model.NoActiveJobsRow{}
	}

	loc := s.now().Location()
	for i := range rows {
		if ls := rows[i].LastService; ls != nil {
			local := *ls
			local.At = ls.At.In(loc)
			rows[i].LastService = &local
		}
	}
	res.Items = rows
	return res, nil
}
